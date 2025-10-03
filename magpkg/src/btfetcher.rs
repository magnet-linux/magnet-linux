use std::{
    fs::{self, File, OpenOptions},
    path::{Path, PathBuf},
    sync::{Arc, mpsc as std_mpsc},
    thread,
    time::{SystemTime, UNIX_EPOCH},
};

use fs2::FileExt;
use librqbit::api::TorrentIdOrHash;
use librqbit::dht::Id20;
use librqbit::{AddTorrent, AddTorrentOptions, ManagedTorrent, Session};
use tokio::runtime::Builder as TokioRuntimeBuilder;
use tokio::sync::mpsc::{self, UnboundedSender};
use tokio::task::JoinHandle;
use tokio::time::{Duration as TokioDuration, interval};

use crate::{MagError, MagResult};

pub const TORRENT_WORK_MARKER: &str = ".torrent-work-";
pub const TORRENT_SESSION_PREFIX: &str = ".torrent-session-";
pub const TORRENT_FETCHER_LOCK: &str = ".torrent-fetcher.lock";

pub struct TorrentFetcher {
    command_tx: UnboundedSender<Command>,
    worker: Option<thread::JoinHandle<()>>,
    session_root: PathBuf,
    work_root: PathBuf,
    _lock_file: File,
}

#[derive(Clone)]
pub struct TorrentDownloadRequest {
    pub url: String,
    pub sha256: String,
    pub filename: String,
    pub dest: PathBuf,
}

pub struct TorrentDownload {
    pub relative_path: PathBuf,
    pub info_hash: String,
    pub torrent_bytes: Vec<u8>,
}

enum Command {
    Download {
        request: TorrentDownloadRequest,
        reply: std_mpsc::Sender<Result<TorrentDownload, String>>,
    },
    Shutdown,
}

impl TorrentFetcher {
    pub fn new(work_root: PathBuf) -> MagResult<Self> {
        fs::create_dir_all(&work_root)?;
        let session_root = allocate_session_dir(&work_root)?;
        fs::create_dir_all(&session_root)?;
        let lock_path = session_root.join(TORRENT_FETCHER_LOCK);
        let lock_file = OpenOptions::new()
            .read(true)
            .write(true)
            .create(true)
            .open(&lock_path)?;
        lock_file.lock_exclusive()?;
        let downloads_root = session_root.join("downloads");
        fs::create_dir_all(&downloads_root)?;

        let (command_tx, command_rx) = mpsc::unbounded_channel();
        let (init_tx, init_rx) = std_mpsc::channel();

        let thread_session_root = session_root.clone();
        let thread_downloads_root = downloads_root.clone();
        let worker = thread::Builder::new()
            .name("torrent-fetcher".into())
            .spawn(move || {
                run_worker(
                    thread_session_root,
                    thread_downloads_root,
                    command_rx,
                    init_tx,
                )
            })
            .map_err(|err| MagError::Generic(format!("failed to spawn torrent fetcher: {err}")))?;

        match init_rx.recv() {
            Ok(Ok(())) => Ok(Self {
                command_tx,
                worker: Some(worker),
                session_root,
                work_root,
                _lock_file: lock_file,
            }),
            Ok(Err(err)) => {
                let _ = command_tx.send(Command::Shutdown);
                let _ = worker.join();
                let _ = fs::remove_dir_all(&session_root);
                Err(MagError::Generic(err))
            }
            Err(err) => {
                let _ = command_tx.send(Command::Shutdown);
                let _ = worker.join();
                let _ = fs::remove_dir_all(&session_root);
                Err(MagError::Generic(format!(
                    "failed to initialise torrent fetcher: {err}"
                )))
            }
        }
    }

    pub fn download(&self, request: TorrentDownloadRequest) -> MagResult<TorrentDownload> {
        let (reply_tx, reply_rx) = std_mpsc::channel();
        self.command_tx
            .send(Command::Download {
                request,
                reply: reply_tx,
            })
            .map_err(|_| MagError::Generic("torrent fetcher thread is not running".into()))?;

        let response = reply_rx
            .recv()
            .map_err(|err| MagError::Generic(format!("torrent fetcher response error: {err}")))?;

        match response {
            Ok(download) => Ok(download),
            Err(msg) => Err(MagError::Generic(msg)),
        }
    }
}

impl Drop for TorrentFetcher {
    fn drop(&mut self) {
        let _ = self.command_tx.send(Command::Shutdown);
        if let Some(handle) = self.worker.take() {
            let _ = handle.join();
        }
        let _ = fs::remove_dir_all(&self.session_root);
        let _ = fs::remove_file(self.work_root.join(TORRENT_FETCHER_LOCK));
    }
}

fn run_worker(
    session_root: PathBuf,
    downloads_root: PathBuf,
    mut command_rx: mpsc::UnboundedReceiver<Command>,
    init_tx: std_mpsc::Sender<Result<(), String>>,
) {
    let runtime = match TokioRuntimeBuilder::new_multi_thread()
        .worker_threads(2)
        .enable_all()
        .build()
    {
        Ok(rt) => rt,
        Err(err) => {
            let _ = init_tx.send(Err(format!("failed to build torrent runtime: {err}")));
            return;
        }
    };

    runtime.block_on(async move {
        let session = match Session::new(session_root.clone()).await {
            Ok(session) => session,
            Err(err) => {
                let _ = init_tx.send(Err(format!("failed to create torrent session: {err:#}")));
                return;
            }
        };

        let _ = init_tx.send(Ok(()));
        let mut counter: u64 = 0;

        while let Some(command) = command_rx.recv().await {
            match command {
                Command::Download { request, reply } => {
                    counter = counter.wrapping_add(1);
                    let result =
                        handle_download(session.clone(), &downloads_root, counter, request)
                            .await
                            .map_err(|err| err.to_string());
                    let _ = reply.send(result);
                }
                Command::Shutdown => break,
            }
        }

        session.stop().await;
    });
}

async fn handle_download(
    session: Arc<Session>,
    downloads_root: &Path,
    counter: u64,
    request: TorrentDownloadRequest,
) -> MagResult<TorrentDownload> {
    let work_dir = allocate_download_dir(downloads_root, &request.sha256, counter)?;
    fs::create_dir_all(&work_dir)?;

    let handle =
        add_torrent_to_session(&session, &work_dir, &request.url, &request.filename).await?;

    let progress = spawn_progress_logger(handle.clone(), request.filename.clone());

    let download_result = handle
        .wait_until_completed()
        .await
        .map_err(|err| MagError::Generic(format!("torrent download failed: {err:#}")));

    progress.abort();
    let _ = progress.await;

    let result = match download_result {
        Ok(_) => {
            finalize_download(
                &session,
                handle,
                &work_dir,
                &request.filename,
                &request.dest,
            )
            .await
        }
        Err(err) => {
            let _ = fs::remove_dir_all(&work_dir);
            Err(err)
        }
    }?;

    match fs::remove_dir_all(&work_dir) {
        Ok(()) => {}
        Err(err) if err.kind() == std::io::ErrorKind::NotFound => {}
        Err(err) => return Err(err.into()),
    }

    Ok(result)
}

async fn add_torrent_to_session(
    session: &Arc<Session>,
    work_dir: &Path,
    url: &str,
    filename: &str,
) -> MagResult<Arc<ManagedTorrent>> {
    let mut opts = AddTorrentOptions::default();
    opts.output_folder = Some(work_dir.to_string_lossy().into_owned());
    opts.overwrite = true;

    let response = session
        .add_torrent(AddTorrent::from_url(url), Some(opts))
        .await
        .map_err(|err| MagError::Generic(format!("failed to add torrent {filename}: {err:#}")))?;

    response.into_handle().ok_or_else(|| {
        MagError::Generic(format!(
            "torrent {filename} added without handle (list-only response)"
        ))
    })
}

fn spawn_progress_logger(handle: Arc<ManagedTorrent>, label: String) -> JoinHandle<()> {
    tokio::spawn(async move {
        let mut ticker = interval(TokioDuration::from_secs(5));
        loop {
            ticker.tick().await;
            let stats = handle.stats();
            let downloaded = stats.progress_bytes;
            let total = stats.total_bytes;

            if total > 0 {
                let percent = (downloaded as f64 / total as f64 * 100.0).min(100.0);
                println!(
                    "torrent {label}: {} / {} ({percent:.1}%)",
                    format_bytes(downloaded as u64),
                    format_bytes(total as u64)
                );
            } else {
                println!(
                    "torrent {label}: {} downloaded",
                    format_bytes(downloaded as u64)
                );
            }

            if stats.finished {
                break;
            }
        }
    })
}

async fn finalize_download(
    session: &Arc<Session>,
    handle: Arc<ManagedTorrent>,
    work_dir: &Path,
    filename: &str,
    dest: &Path,
) -> MagResult<TorrentDownload> {
    let torrent_bytes = handle
        .with_metadata(|meta| meta.torrent_bytes.clone())
        .map_err(|err| {
            MagError::Generic(format!("missing torrent metadata for {filename}: {err:#}"))
        })?
        .to_vec();

    let file_infos = handle
        .with_metadata(|meta| meta.file_infos.clone())
        .map_err(|err| MagError::Generic(format!("missing file info for {filename}: {err:#}")))?;

    if file_infos.len() != 1 {
        return Err(MagError::Generic(format!(
            "torrent for {filename} contained {} files (expected 1)",
            file_infos.len()
        )));
    }

    let relative = PathBuf::from(file_infos[0].relative_filename.clone());
    let downloaded_path = work_dir.join(&relative);

    if !downloaded_path.exists() {
        return Err(MagError::Generic(format!(
            "torrent download for {filename} missing payload at {}",
            downloaded_path.display()
        )));
    }

    let info_hash = format_hex(handle.info_hash());

    fs::copy(&downloaded_path, dest)?;

    if let Err(err) = session
        .delete(TorrentIdOrHash::from(handle.id()), false)
        .await
    {
        println!(
            "warning: failed to remove torrent {} from session: {err:#}",
            info_hash
        );
    }

    Ok(TorrentDownload {
        relative_path: relative,
        info_hash,
        torrent_bytes,
    })
}

fn allocate_session_dir(work_root: &Path) -> MagResult<PathBuf> {
    let mut rng_seed = SystemTime::now()
        .duration_since(UNIX_EPOCH)
        .map(|d| d.as_nanos())
        .unwrap_or(0);

    for _ in 0..1_000 {
        rng_seed = rng_seed.wrapping_mul(6364136223846793005).wrapping_add(1);
        let suffix = format!("{:016x}", rng_seed ^ (std::process::id() as u128));
        let candidate = work_root.join(format!("{TORRENT_SESSION_PREFIX}{suffix}"));
        if !candidate.exists() {
            return Ok(candidate);
        }
    }

    Err(MagError::Generic(
        "unable to allocate torrent session workspace".into(),
    ))
}

fn allocate_download_dir(downloads_root: &Path, sha: &str, counter: u64) -> MagResult<PathBuf> {
    let dir = downloads_root.join(format!("{sha}{TORRENT_WORK_MARKER}{counter:016x}"));
    if dir.exists() {
        match fs::remove_dir_all(&dir) {
            Ok(()) => {}
            Err(err) if err.kind() == std::io::ErrorKind::NotFound => {}
            Err(err) => return Err(err.into()),
        }
    }
    Ok(dir)
}

fn format_bytes(bytes: u64) -> String {
    const UNITS: [&str; 5] = ["B", "KB", "MB", "GB", "TB"];
    let mut value = bytes as f64;
    let mut unit_index = 0;
    while value >= 1024.0 && unit_index < UNITS.len() - 1 {
        value /= 1024.0;
        unit_index += 1;
    }
    if unit_index == 0 {
        format!("{} {}", bytes, UNITS[unit_index])
    } else {
        format!("{value:.1} {}", UNITS[unit_index])
    }
}

fn format_hex(id: Id20) -> String {
    hex::encode(id.0)
}
