use std::{
    collections::{HashMap, HashSet},
    fs::{self, File},
    io::ErrorKind,
    path::{Path, PathBuf},
    str,
    sync::Arc,
    time::{Duration, SystemTime},
};

use fs2::FileExt;
use librqbit::dht::{Id20, PersistentDhtConfig};
use librqbit::{
    AddTorrent, AddTorrentOptions, AddTorrentResponse, ByteBufOwned, ManagedTorrent, ParsedTorrent,
    Session, SessionOptions, torrent_from_bytes_ext,
};
use tokio::runtime::Builder as TokioRuntimeBuilder;
use tokio::signal;
use tokio::time::{Duration as TokioDuration, interval};

use crate::{MagError, MagResult};

pub struct TorrentSeeder {
    torrent_root: PathBuf,
    seed_root: PathBuf,
}

pub struct SeedLock {
    _file: File,
}

pub struct TorrentSeedInfo {
    pub info_hash: String,
    pub relative_path: PathBuf,
    pub bytes: Vec<u8>,
}

struct ActiveSeed {
    handle: Arc<ManagedTorrent>,
    display_name: String,
    torrent_dir: PathBuf,
}

struct SeedPlan {
    info_hash: String,
    display_name: String,
    torrent_dir: PathBuf,
    torrent_bytes: Vec<u8>,
}

impl TorrentSeeder {
    pub fn new(watch_dir: impl Into<PathBuf>, state_dir: impl Into<PathBuf>) -> MagResult<Self> {
        let torrent_root = watch_dir.into();
        if torrent_root.as_os_str().is_empty() {
            return Err(MagError::Generic(
                "torrent seeder requires a directory to watch".into(),
            ));
        }

        fs::create_dir_all(&torrent_root)?;

        let seed_root = state_dir.into();
        if seed_root.as_os_str().is_empty() {
            return Err(MagError::Generic(
                "torrent seeder requires a state directory".into(),
            ));
        }
        fs::create_dir_all(&seed_root)?;

        Ok(Self {
            torrent_root,
            seed_root,
        })
    }

    pub fn run(&self, expiry: Duration, listen_port: Option<u16>) -> MagResult<()> {
        let lock = acquire_seed_lock(&self.seed_root)?;
        println!(
            "seeder lock acquired at {}",
            self.seed_root.join("seeder.lock").display()
        );

        let runtime = TokioRuntimeBuilder::new_multi_thread()
            .worker_threads(2)
            .enable_all()
            .build()
            .map_err(|err| MagError::Generic(format!("failed to build tokio runtime: {err}")))?;

        let result = runtime.block_on(self.run_seed_loop(expiry, listen_port));

        drop(lock);
        result
    }

    async fn run_seed_loop(&self, expiry: Duration, listen_port: Option<u16>) -> MagResult<()> {
        let mut session_opts = SessionOptions::default();
        session_opts.dht_config = Some(PersistentDhtConfig {
            dump_interval: Some(Duration::from_secs(60)),
            config_filename: Some(self.seed_root.join("dht.json")),
        });

        if let Some(port) = listen_port {
            if port == u16::MAX {
                return Err(MagError::Generic(
                    "listen port must be lower than 65535".into(),
                ));
            }
            session_opts.listen_port_range = Some(port..(port + 1));
        }

        let session = Session::new_with_opts(self.torrent_root.clone(), session_opts)
            .await
            .map_err(|err| {
                MagError::Generic(format!("failed to start seeding session: {err:#}"))
            })?;

        if let Some(port) = session.tcp_listen_port() {
            println!("seeder listening on TCP port {port}");
        } else {
            println!("seeder running without TCP listener");
        }
        println!("torrent seeder started; press Ctrl+C to stop");

        let mut active: HashMap<String, ActiveSeed> = HashMap::new();
        if let Err(err) = self
            .sync_seeding_iteration(&session, &mut active, expiry)
            .await
        {
            println!("initial seeding scan error: {err:#}");
        }

        let mut ticker = interval(TokioDuration::from_secs(15));
        loop {
            tokio::select! {
                _ = signal::ctrl_c() => {
                    println!("interrupt received, shutting down seeder...");
                    break;
                }
                _ = ticker.tick() => {
                    if let Err(err) = self.sync_seeding_iteration(&session, &mut active, expiry).await {
                        println!("seeding loop error: {err:#}");
                    }
                }
            }
        }

        for (info_hash, active_seed) in active.iter() {
            if let Err(err) = session.pause(&active_seed.handle).await {
                println!(
                    "warning: failed to pause torrent {info_hash} ({}): {err:#}",
                    active_seed.display_name
                );
            }
        }

        session.stop().await;
        println!("seeder exited");
        Ok(())
    }

    async fn sync_seeding_iteration(
        &self,
        session: &Arc<Session>,
        active: &mut HashMap<String, ActiveSeed>,
        expiry: Duration,
    ) -> MagResult<()> {
        let now = SystemTime::now();
        let (plans, warnings, expired_dirs) =
            scan_torrent_directory(self.torrent_root.clone(), now, expiry)?;

        for warning in warnings {
            println!("seeder: {warning}");
        }

        let seen: HashSet<String> = plans.iter().map(|p| p.info_hash.clone()).collect();
        let expired_set: HashSet<PathBuf> = expired_dirs.into_iter().collect();

        for dir in &expired_set {
            if !active
                .values()
                .any(|seed| seed.torrent_dir.as_path() == dir.as_path())
            {
                match fs::remove_dir_all(dir) {
                    Ok(()) => println!("seeder: removed expired torrent dir {}", dir.display()),
                    Err(err) if err.kind() == ErrorKind::NotFound => {}
                    Err(err) => println!(
                        "seeder: failed to remove expired torrent dir {}: {err:#}",
                        dir.display()
                    ),
                }
            }
        }

        let mut to_remove = Vec::new();
        for (info_hash, active_seed) in active.iter() {
            if !seen.contains(info_hash) {
                to_remove.push(info_hash.clone());
            }
            if expired_set.contains(&active_seed.torrent_dir) {
                to_remove.push(info_hash.clone());
            }
        }

        for info_hash in to_remove {
            if let Some(active_seed) = active.remove(&info_hash) {
                println!(
                    "seeder: stopping {info_hash} ({})",
                    active_seed.display_name
                );
                if let Err(err) = session.pause(&active_seed.handle).await {
                    println!("warning: failed to pause torrent {info_hash}: {err:#}");
                }
                if expired_set.contains(&active_seed.torrent_dir) {
                    match fs::remove_dir_all(&active_seed.torrent_dir) {
                        Ok(()) => println!(
                            "seeder: cleaned expired torrent dir {}",
                            active_seed.torrent_dir.display()
                        ),
                        Err(err) if err.kind() == ErrorKind::NotFound => {}
                        Err(err) => println!(
                            "seeder: failed to remove expired torrent dir {}: {err:#}",
                            active_seed.torrent_dir.display()
                        ),
                    }
                }
            }
        }

        for plan in plans {
            if active.contains_key(&plan.info_hash) {
                continue;
            }

            let SeedPlan {
                info_hash,
                display_name,
                torrent_dir,
                torrent_bytes,
            } = plan;

            let mut opts = AddTorrentOptions::default();
            opts.paused = false;
            // Allow librqbit to adopt the existing on-disk payload instead of
            // failing with EEXIST when the file is already present.
            opts.overwrite = true;
            opts.output_folder = Some(torrent_dir.to_string_lossy().into_owned());

            match session
                .add_torrent(AddTorrent::from_bytes(torrent_bytes), Some(opts))
                .await
            {
                Ok(AddTorrentResponse::Added(_, handle))
                | Ok(AddTorrentResponse::AlreadyManaged(_, handle)) => {
                    if let Err(err) = session.unpause(&handle).await {
                        println!("warning: failed to unpause torrent {info_hash}: {err:#}");
                        continue;
                    }
                    println!("seeder: now seeding {info_hash} ({display_name})");
                    active.insert(
                        info_hash,
                        ActiveSeed {
                            handle,
                            display_name,
                            torrent_dir,
                        },
                    );
                }
                Ok(AddTorrentResponse::ListOnly(_)) => {
                    println!(
                        "warning: torrent {info_hash} ({display_name}) returned list-only response"
                    );
                }
                Err(err) => {
                    println!(
                        "warning: failed to add torrent {info_hash} ({display_name}): {err:#}"
                    );
                }
            }
        }

        Ok(())
    }
}

pub fn try_acquire_seed_lock(seed_root: &Path) -> MagResult<Option<SeedLock>> {
    fs::create_dir_all(seed_root)?;
    let lock_path = seed_root.join("seeder.lock");
    let lock_file = File::create(&lock_path)?;
    match lock_file.try_lock_exclusive() {
        Ok(()) => Ok(Some(SeedLock { _file: lock_file })),
        Err(err) if err.kind() == ErrorKind::WouldBlock => Ok(None),
        Err(err) => Err(err.into()),
    }
}

fn acquire_seed_lock(seed_root: &Path) -> MagResult<SeedLock> {
    fs::create_dir_all(seed_root)?;
    let lock_path = seed_root.join("seeder.lock");
    let lock_file = File::create(&lock_path)?;
    lock_file.lock_exclusive()?;
    Ok(SeedLock { _file: lock_file })
}

pub fn load_torrent_seed_info(torrent_path: &Path) -> MagResult<TorrentSeedInfo> {
    let bytes = fs::read(torrent_path)?;
    let parsed: ParsedTorrent<ByteBufOwned> = torrent_from_bytes_ext(&bytes).map_err(|err| {
        MagError::Generic(format!(
            "failed to parse torrent metadata from {}: {err:#}",
            torrent_path.display()
        ))
    })?;

    let info_hash = info_hash_to_hex(parsed.meta.info_hash);
    let info = parsed.meta.info;

    let relative_path = if let Some(files) = info.files {
        if files.len() != 1 {
            return Err(MagError::Generic(format!(
                "torrent {} referenced {} files (expected 1)",
                torrent_path.display(),
                files.len()
            )));
        }
        let mut path = PathBuf::new();
        files[0].full_path(&mut path).map_err(|err| {
            MagError::Generic(format!(
                "invalid torrent file path in {}: {err:#}",
                torrent_path.display()
            ))
        })?;
        path
    } else if let Some(name) = info.name {
        let name_str = str::from_utf8(name.as_ref()).map_err(|err| {
            MagError::Generic(format!(
                "invalid torrent name in {}: {err:#}",
                torrent_path.display()
            ))
        })?;
        PathBuf::from(name_str)
    } else {
        return Err(MagError::Generic(format!(
            "torrent {} missing file name metadata",
            torrent_path.display()
        )));
    };

    if relative_path.components().next().is_none() {
        return Err(MagError::Generic(format!(
            "torrent {} does not contain a valid path",
            torrent_path.display()
        )));
    }

    Ok(TorrentSeedInfo {
        info_hash,
        relative_path,
        bytes,
    })
}

fn scan_torrent_directory(
    torrent_root: PathBuf,
    now: SystemTime,
    expiry: Duration,
) -> MagResult<(Vec<SeedPlan>, Vec<String>, Vec<PathBuf>)> {
    let mut plans = Vec::new();
    let mut warnings = Vec::new();
    let mut expired_dirs = Vec::new();

    for entry in fs::read_dir(&torrent_root)? {
        let entry = entry?;
        if !entry.file_type()?.is_dir() {
            continue;
        }

        let dir_path = entry.path();
        let torrent_path = dir_path.join("resource.torrent");
        if !torrent_path.exists() {
            continue;
        }

        let metadata = fs::metadata(&torrent_path)?;
        if is_metadata_expired(&metadata, now, expiry) {
            warnings.push(format!("skipping expired torrent {}", dir_path.display()));
            expired_dirs.push(dir_path.clone());
            continue;
        }

        let seed_info = match load_torrent_seed_info(&torrent_path) {
            Ok(info) => info,
            Err(err) => {
                warnings.push(format!(
                    "failed to read {}: {err:#}",
                    torrent_path.display()
                ));
                continue;
            }
        };

        let data_path = dir_path.join(&seed_info.relative_path);
        if !data_path.exists() {
            warnings.push(format!(
                "skipping torrent {}: payload missing at {}",
                seed_info.info_hash,
                data_path.display()
            ));
            continue;
        }

        let display_name = seed_info.relative_path.display().to_string();
        plans.push(SeedPlan {
            info_hash: seed_info.info_hash,
            display_name,
            torrent_dir: dir_path,
            torrent_bytes: seed_info.bytes,
        });
    }

    Ok((plans, warnings, expired_dirs))
}

fn info_hash_to_hex(id: Id20) -> String {
    hex::encode(id.0)
}

fn is_metadata_expired(metadata: &fs::Metadata, now: SystemTime, expiry: Duration) -> bool {
    match metadata.modified() {
        Ok(modified) => match now.duration_since(modified) {
            Ok(age) => age > expiry,
            Err(_) => false,
        },
        Err(_) => false,
    }
}

impl Drop for SeedLock {
    fn drop(&mut self) {}
}
