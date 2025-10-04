use std::{
    collections::{HashMap, HashSet, VecDeque},
    env,
    fs::{self, File, OpenOptions},
    io::{self, ErrorKind, Read, Write},
    os::unix::fs::PermissionsExt,
    path::{Path, PathBuf},
    process::Command,
    rc::Rc,
    sync::{Arc, Mutex},
    time::{Duration, Instant, SystemTime},
};

use filetime::{FileTime, set_file_times};
use flate2::read::GzDecoder;
use fs2::FileExt;
use reqwest::{Url, blocking::Client};
use sha2::{Digest, Sha256};
use tar::{Builder, EntryType};
use tokio::runtime::Builder as TokioRuntimeBuilder;
use zstd::stream::{read::Decoder as ZstdDecoder, write::Encoder as ZstdEncoder};

use crate::{
    MagError, MagResult,
    btfetcher::{
        TORRENT_FETCHER_LOCK, TORRENT_SESSION_PREFIX, TORRENT_WORK_MARKER, TorrentDownloadRequest,
        TorrentFetcher,
    },
    btseed::{self, TorrentSeedInfo, load_torrent_seed_info},
    package::{FetchResource, Package},
};

use librqbit::dht::Id20;
use librqbit::{CreateTorrentOptions, Magnet, create_torrent};

const FETCH_LOCK_SUFFIX: &str = ".lock";
pub struct PackageStore {
    client: Client,
    base_root: PathBuf,
    store_root: PathBuf,
    fetch_root: PathBuf,
    torrent_root: PathBuf,
    torrent_fetcher: Mutex<Option<Arc<TorrentFetcher>>>,
}

#[derive(Default, Debug)]
pub struct CleanupStats {
    pub package_artifacts_removed: usize,
    pub package_build_dirs_removed: usize,
    pub package_lock_files_removed: usize,
    pub fetch_files_removed: usize,
    pub fetch_partials_removed: usize,
    pub fetch_lock_files_removed: usize,
    pub torrent_dirs_removed: usize,
    pub torrent_work_dirs_removed: usize,
    pub torrent_session_dirs_removed: usize,
}

struct TorrentInfo {
    info_hash: String,
    relative_path: PathBuf,
    torrent_bytes: Vec<u8>,
}

struct DownloadOutcome {
    path: PathBuf,
    torrent: Option<TorrentInfo>,
}

impl PackageStore {
    pub fn new() -> MagResult<Self> {
        let base_root = if let Some(custom) = env::var_os("MAGPKG_STORE") {
            PathBuf::from(custom)
        } else {
            let home = env::var_os("HOME")
                .ok_or_else(|| MagError::Generic("HOME environment variable is not set".into()))?;
            PathBuf::from(home).join(".magpkg")
        };
        let fetch_root = base_root.join("fetch");
        let store_root = base_root.join("pkgs");
        let torrent_root = base_root.join("torrent");
        fs::create_dir_all(&fetch_root)?;
        fs::create_dir_all(&store_root)?;
        fs::create_dir_all(&torrent_root)?;

        let user_agent = format!("magpkg/{}", env!("CARGO_PKG_VERSION"));

        let client = Client::builder()
            .timeout(Duration::from_secs(12 * 60 * 60))
            .user_agent(&user_agent)
            .build()?;

        Ok(Self {
            client,
            base_root,
            store_root,
            fetch_root,
            torrent_root,
            torrent_fetcher: Mutex::new(None),
        })
    }

    pub fn build_packages(
        &self,
        roots: &[Rc<Package>],
        parallelism: usize,
    ) -> MagResult<Vec<PathBuf>> {
        let parallelism = parallelism.max(1);
        let mut order = Vec::new();
        let mut visited = HashSet::new();
        for pkg in roots {
            collect_closure(pkg.clone(), &mut visited, &mut order);
        }

        let mut artifacts = Vec::with_capacity(order.len());
        for package in order {
            let path = self.build_single(&package, parallelism)?;
            artifacts.push(path);
        }
        self.shutdown_torrent_fetcher()?;
        Ok(artifacts)
    }

    pub fn cleanup(&self, expiry: Duration) -> MagResult<CleanupStats> {
        let now = SystemTime::now();
        let mut stats = CleanupStats::default();
        self.cleanup_packages(now, expiry, &mut stats)?;
        self.cleanup_fetches(now, expiry, &mut stats)?;
        let seed_root = self.seed_root();
        match btseed::try_acquire_seed_lock(&seed_root)? {
            Some(_lock) => {
                self.cleanup_torrents(now, expiry, &mut stats)?;
            }
            None => {
                println!("Skipping torrent cleanup; seeder appears to be running.");
            }
        }
        Ok(stats)
    }

    pub fn fetch_packages(&self, roots: &[Rc<Package>], missing_only: bool) -> MagResult<()> {
        let mut queue = VecDeque::new();
        let mut visited = HashSet::new();

        for pkg in roots {
            queue.push_back(pkg.clone());
        }

        while let Some(pkg) = queue.pop_front() {
            if !visited.insert(pkg.hash.clone()) {
                continue;
            }

            for dep in pkg.run_deps.iter().chain(pkg.build_deps.iter()) {
                queue.push_back(dep.clone());
            }

            if missing_only {
                let artifact = self.package_artifact_path(pkg.as_ref());
                if artifact.exists() {
                    continue;
                }
            }

            if pkg.fetch.is_empty() {
                continue;
            }

            let base = package_base_name(pkg.as_ref());
            println!("fetching sources for {base}...");
            for fetch in &pkg.fetch {
                self.cache_fetch(fetch)?;
            }
        }

        self.shutdown_torrent_fetcher()?;
        Ok(())
    }

    fn torrent_fetcher(&self) -> MagResult<Arc<TorrentFetcher>> {
        let mut guard = self
            .torrent_fetcher
            .lock()
            .map_err(|_| MagError::Generic("torrent fetcher mutex poisoned".into()))?;

        if let Some(fetcher) = guard.as_ref() {
            return Ok(fetcher.clone());
        }

        let fetcher = Arc::new(TorrentFetcher::new(self.fetch_root.clone())?);
        *guard = Some(fetcher.clone());
        Ok(fetcher)
    }

    fn shutdown_torrent_fetcher(&self) -> MagResult<()> {
        let mut guard = self
            .torrent_fetcher
            .lock()
            .map_err(|_| MagError::Generic("torrent fetcher mutex poisoned".into()))?;
        guard.take();
        Ok(())
    }

    pub fn seed_root(&self) -> PathBuf {
        self.base_root.join("seed")
    }

    pub fn torrent_root(&self) -> &Path {
        &self.torrent_root
    }

    fn build_single(&self, package: &Rc<Package>, parallelism: usize) -> MagResult<PathBuf> {
        let base = package_base_name(package.as_ref());
        let artifact_path = self.store_root.join(format!("{base}.tar.zst"));
        let lock_path = self.store_root.join(format!("{base}.lock"));
        let lock_file = File::create(&lock_path)?;
        lock_file.lock_exclusive()?;

        if artifact_path.exists() {
            touch_path(&artifact_path)?;
            touch_path(&lock_path)?;
            return Ok(artifact_path);
        }

        println!("building {base}...");

        let build_root = self.store_root.join(format!("{base}.build"));
        if build_root.exists() {
            fs::remove_dir_all(&build_root)?;
        }
        fs::create_dir_all(&build_root)?;

        if package.build == "untar" {
            let fetch_dir = build_root.join("fetch");
            let out_dir = build_root.join("untar-out");

            clear_directory(&fetch_dir)?;
            clear_directory(&out_dir)?;

            let fetch_files = self.prepare_fetches(&package.fetch, &fetch_dir)?;
            build_via_untar(&fetch_files, &out_dir)?;

            pack_output(&out_dir, &artifact_path)?;
            touch_path(&artifact_path)?;
            touch_path(&lock_path)?;
            fs::remove_dir_all(&build_root)?;

            return Ok(artifact_path);
        }

        let rootfs = build_root.join("rootfs");
        fs::create_dir_all(&rootfs)?;

        self.install_dependencies_into_root(package.as_ref(), &rootfs)?;

        for dir in ["dev", "proc", "sys", "tmp"] {
            let path = rootfs.join(dir);
            if fs::symlink_metadata(&path).is_err() {
                fs::create_dir_all(path)?;
            }
        }

        let out_dir = rootfs.join("out");
        let fetch_dir = rootfs.join("fetch");
        let store_dir = rootfs.join("store");
        let build_dir = rootfs.join("build");

        clear_directory(&out_dir)?;
        clear_directory(&fetch_dir)?;
        clear_directory(&store_dir)?;
        clear_directory(&build_dir)?;

        self.populate_build_store(package, &store_dir)?;
        self.prepare_fetches(&package.fetch, &fetch_dir)?;

        run_bwrap_build(package.as_ref(), &rootfs, parallelism)?;

        pack_output(&out_dir, &artifact_path)?;
        touch_path(&artifact_path)?;
        touch_path(&lock_path)?;
        fs::remove_dir_all(&build_root)?;

        Ok(artifact_path)
    }

    fn cleanup_packages(
        &self,
        now: SystemTime,
        expiry: Duration,
        stats: &mut CleanupStats,
    ) -> MagResult<()> {
        let mut bases = HashSet::new();
        for entry in fs::read_dir(&self.store_root)? {
            let entry = entry?;
            let name = entry.file_name();
            if let Some(base) = package_base_from_entry(&name.to_string_lossy()) {
                bases.insert(base);
            }
        }

        for base in bases {
            let lock_path = self.store_root.join(format!("{base}.lock"));
            let lock_file = OpenOptions::new()
                .read(true)
                .write(true)
                .create(true)
                .open(&lock_path)?;

            match lock_file.try_lock_exclusive() {
                Ok(()) => {}
                Err(err) if err.kind() == ErrorKind::WouldBlock => {
                    // Another process is using this package; skip cleanup for it.
                    continue;
                }
                Err(err) => return Err(err.into()),
            }

            let artifact_path = self.store_root.join(format!("{base}.tar.zst"));
            if remove_path_if_expired(&artifact_path, now, expiry)? {
                stats.package_artifacts_removed += 1;
            }

            let build_path = self.store_root.join(format!("{base}.build"));
            if build_path.exists() {
                fs::remove_dir_all(&build_path)?;
                stats.package_build_dirs_removed += 1;
            }

            let mut remove_lock = false;
            if !artifact_path.exists() && !build_path.exists() {
                if is_path_expired(&lock_path, now, expiry)? {
                    remove_lock = true;
                }
            }

            drop(lock_file);

            if remove_lock && lock_path.exists() {
                fs::remove_file(&lock_path)?;
                stats.package_lock_files_removed += 1;
            }
        }

        Ok(())
    }

    fn install_dependencies_into_root(&self, package: &Package, rootfs: &Path) -> MagResult<()> {
        fn visit(package: &Rc<Package>, seen: &mut HashSet<String>, order: &mut Vec<Rc<Package>>) {
            if !seen.insert(package.hash.clone()) {
                return;
            }

            for child in package.build_deps.iter().chain(package.run_deps.iter()) {
                visit(child, seen, order);
            }

            order.push(package.clone());
        }

        let mut seen = HashSet::new();
        let mut order = Vec::new();

        for dep in package.build_deps.iter().chain(package.run_deps.iter()) {
            visit(dep, &mut seen, &mut order);
        }

        for dep in order {
            let artifact = self.package_artifact_path(dep.as_ref());
            if !artifact.exists() {
                return Err(MagError::Generic(format!(
                    "missing artifact for dependency {}",
                    dep.hash
                )));
            }

            extract_tar_zst(&artifact, rootfs)?;
        }

        Ok(())
    }

    fn cleanup_fetches(
        &self,
        now: SystemTime,
        expiry: Duration,
        stats: &mut CleanupStats,
    ) -> MagResult<()> {
        #[derive(Default)]
        struct FetchGroup {
            file: Option<PathBuf>,
            partials: Vec<PathBuf>,
            work_dirs: Vec<PathBuf>,
        }

        struct SessionInfo {
            path: PathBuf,
            lock: Option<File>,
            active: bool,
        }

        let mut active_session_present = false;
        let mut session_infos = Vec::new();

        let mut groups = HashMap::<String, FetchGroup>::new();
        let mut orphan_work_dirs = Vec::new();
        for entry in fs::read_dir(&self.fetch_root)? {
            let entry = entry?;
            let path = entry.path();
            let name = entry.file_name();
            let name_str = name.to_string_lossy();

            let file_type = entry.file_type()?;

            if file_type.is_dir() {
                let raw_name = name_str.as_ref();
                if let Some((base, _)) = raw_name.split_once(TORRENT_WORK_MARKER) {
                    let group = groups.entry(base.to_string()).or_default();
                    group.work_dirs.push(path.clone());
                    orphan_work_dirs.push(path.clone());
                    continue;
                }
                if raw_name.starts_with(TORRENT_SESSION_PREFIX) {
                    let lock_path = path.join(TORRENT_FETCHER_LOCK);
                    let mut lock = None;
                    let mut active = false;
                    if lock_path.exists() {
                        match OpenOptions::new().read(true).write(true).open(&lock_path) {
                            Ok(file) => match file.try_lock_exclusive() {
                                Ok(()) => {
                                    lock = Some(file);
                                }
                                Err(err) if err.kind() == ErrorKind::WouldBlock => {
                                    active = true;
                                    active_session_present = true;
                                }
                                Err(err) => return Err(err.into()),
                            },
                            Err(err) if err.kind() == ErrorKind::NotFound => {}
                            Err(err) => return Err(err.into()),
                        }
                    }
                    session_infos.push(SessionInfo {
                        path: path.clone(),
                        lock,
                        active,
                    });
                    continue;
                }
                continue;
            }

            if !file_type.is_file() {
                continue;
            }

            if name_str == TORRENT_FETCHER_LOCK {
                if remove_path_if_expired(&path, now, expiry)? {
                    stats.fetch_lock_files_removed += 1;
                }
                continue;
            }

            if let Some(base) = name_str.strip_suffix(FETCH_LOCK_SUFFIX) {
                groups.entry(base.to_string()).or_default();
                continue;
            }

            if let Some(base) = name_str.strip_suffix(".tmp") {
                groups
                    .entry(base.to_string())
                    .or_default()
                    .partials
                    .push(path);
                continue;
            }

            // Treat as content-addressed fetch file.
            groups.entry(name_str.to_string()).or_default().file = Some(path);
        }

        for (base, group) in groups {
            let lock_path = self.fetch_root.join(format!("{base}{FETCH_LOCK_SUFFIX}"));
            let lock_file = OpenOptions::new()
                .read(true)
                .write(true)
                .create(true)
                .open(&lock_path)?;
            match lock_file.try_lock_exclusive() {
                Ok(()) => {}
                Err(err) if err.kind() == ErrorKind::WouldBlock => {
                    continue;
                }
                Err(err) => return Err(err.into()),
            }

            let mut file_exists = false;
            if let Some(file_path) = &group.file {
                if is_path_expired(file_path, now, expiry)? {
                    match fs::remove_file(file_path) {
                        Ok(()) => stats.fetch_files_removed += 1,
                        Err(err) if err.kind() == ErrorKind::NotFound => {}
                        Err(err) => return Err(err.into()),
                    }
                } else if file_path.exists() {
                    file_exists = true;
                }
            }

            let mut partials_remaining = false;
            for partial_path in group.partials {
                let removed = remove_path_if_expired(&partial_path, now, expiry)?;
                if removed {
                    stats.fetch_partials_removed += 1;
                } else if partial_path.exists() {
                    partials_remaining = true;
                }
            }

            for work_dir in group.work_dirs {
                if active_session_present {
                    if work_dir.exists() {
                        partials_remaining = true;
                    }
                    continue;
                }
                let removed = remove_path_if_expired(&work_dir, now, expiry)?;
                if removed {
                    stats.fetch_partials_removed += 1;
                    stats.torrent_work_dirs_removed += 1;
                } else if work_dir.exists() {
                    partials_remaining = true;
                }
            }

            let mut remove_lock = false;
            if !file_exists && !partials_remaining {
                if is_path_expired(&lock_path, now, expiry)? {
                    remove_lock = true;
                }
            }

            drop(lock_file);
            if remove_lock && lock_path.exists() {
                fs::remove_file(&lock_path)?;
                stats.fetch_lock_files_removed += 1;
            }
        }

        if !active_session_present {
            for work_dir in orphan_work_dirs {
                if remove_path_if_expired(&work_dir, now, expiry)? {
                    stats.fetch_partials_removed += 1;
                    stats.torrent_work_dirs_removed += 1;
                }
            }
        }

        for session in session_infos {
            let SessionInfo {
                path,
                mut lock,
                active,
            } = session;

            if active {
                continue;
            }

            let downloads_dir = path.join("downloads");
            if downloads_dir.exists() {
                for entry in fs::read_dir(&downloads_dir)? {
                    let entry = entry?;
                    if !entry.file_type()?.is_dir() {
                        continue;
                    }
                    let entry_path = entry.path();
                    let removed = remove_path_if_expired(&entry_path, now, expiry)?;
                    if removed {
                        stats.fetch_partials_removed += 1;
                        stats.torrent_work_dirs_removed += 1;
                    }
                }
            }

            drop(lock.take());

            if remove_path_if_expired(&path, now, expiry)? {
                stats.torrent_session_dirs_removed += 1;
            }
        }

        Ok(())
    }

    fn cleanup_torrents(
        &self,
        now: SystemTime,
        expiry: Duration,
        stats: &mut CleanupStats,
    ) -> MagResult<()> {
        for entry in fs::read_dir(&self.torrent_root)? {
            let entry = entry?;
            if !entry.file_type()?.is_dir() {
                continue;
            }
            let path = entry.path();
            let metadata = fs::metadata(&path)?;
            if is_metadata_expired(&metadata, now, expiry) {
                match fs::remove_dir_all(&path) {
                    Ok(()) => stats.torrent_dirs_removed += 1,
                    Err(err) if err.kind() == ErrorKind::NotFound => {}
                    Err(err) => return Err(err.into()),
                }
            }
        }
        Ok(())
    }

    fn populate_build_store(&self, package: &Package, store_dir: &Path) -> MagResult<()> {
        let mut queue = VecDeque::new();
        let mut seen = HashSet::new();
        for dep in &package.build_deps {
            queue.push_back(dep.clone());
        }

        while let Some(dep) = queue.pop_front() {
            if !seen.insert(dep.hash.clone()) {
                continue;
            }

            // Ensure the dependency artifact exists.
            let artifact = self.package_artifact_path(dep.as_ref());
            if !artifact.exists() {
                return Err(MagError::Generic(format!(
                    "missing artifact for dependency {}",
                    dep.hash
                )));
            }

            let dest = store_dir.join(package_base_name(dep.as_ref()));
            if dest.exists() {
                fs::remove_dir_all(&dest)?;
            }
            fs::create_dir_all(&dest)?;
            extract_tar_zst(&artifact, &dest)?;

            for run_dep in &dep.run_deps {
                queue.push_back(run_dep.clone());
            }
            for build_dep in &dep.build_deps {
                queue.push_back(build_dep.clone());
            }
        }

        Ok(())
    }

    fn prepare_fetches(
        &self,
        fetches: &[FetchResource],
        fetch_dir: &Path,
    ) -> MagResult<Vec<PathBuf>> {
        let mut result = Vec::with_capacity(fetches.len());
        for fetch in fetches {
            let cached = self.cache_fetch(fetch)?;
            let dest = fetch_dir.join(&fetch.filename);
            fs::copy(&cached, &dest)?;
            result.push(dest);
        }
        Ok(result)
    }

    fn cache_fetch(&self, fetch: &FetchResource) -> MagResult<PathBuf> {
        let dest = self.fetch_root.join(&fetch.sha256);
        let lock_path = self
            .fetch_root
            .join(format!("{}{}", fetch.sha256, FETCH_LOCK_SUFFIX));
        let lock_file = File::create(&lock_path)?;
        lock_file.lock_exclusive()?;

        let result = self.cache_fetch_locked(fetch, &dest);

        touch_path(&lock_path)?;
        drop(lock_file);

        result
    }

    fn cache_fetch_locked(&self, fetch: &FetchResource, dest: &Path) -> MagResult<PathBuf> {
        if dest.exists() {
            if verify_sha256(dest, &fetch.sha256)? {
                println!("fetch cache hit: {} ({})", fetch.filename, fetch.sha256);
                touch_path(dest)?;
                self.refresh_torrent_artifacts(fetch, dest)?;
                return Ok(dest.to_path_buf());
            }
            fs::remove_file(dest)?;
        }

        if fetch.urls.is_empty() {
            return Err(MagError::Generic(format!(
                "no URLs provided for fetch {}",
                fetch.filename
            )));
        }

        let mut prioritized_urls: Vec<&str> = Vec::with_capacity(fetch.urls.len());
        for url in &fetch.urls {
            if is_torrent_url(url) {
                prioritized_urls.push(url.as_str());
            }
        }
        for url in &fetch.urls {
            if !is_torrent_url(url) {
                prioritized_urls.push(url.as_str());
            }
        }

        let mut last_err: Option<MagError> = None;

        for url in prioritized_urls {
            println!("fetching {} from {}", fetch.filename, url);
            let outcome = self.fetch_url(fetch, url, dest);

            match outcome {
                Ok(mut download) => {
                    let tmp_path = download.path.clone();
                    let hash_ok = verify_sha256(&tmp_path, &fetch.sha256)?;
                    if !hash_ok {
                        last_err = Some(MagError::Generic(format!(
                            "SHA mismatch for {}",
                            fetch.filename
                        )));
                        let _ = fs::remove_file(&tmp_path);
                        if let Some(_info) = download.torrent.take() {
                            // nothing to persist when hash fails; drop bytes
                        }
                        continue;
                    }

                    if dest.exists() {
                        fs::remove_file(dest)?;
                    }
                    fs::rename(&tmp_path, dest)?;
                    File::open(dest)?.sync_all()?;
                    let final_path = dest.to_path_buf();
                    println!("fetch complete: {} ({})", fetch.filename, fetch.sha256);
                    touch_path(&final_path)?;

                    let torrent_info = match download.torrent.take() {
                        Some(info) => info,
                        None => self.create_torrent_for_file(fetch, &final_path)?,
                    };
                    self.write_torrent_artifacts(fetch, &final_path, &torrent_info)?;
                    return Ok(final_path);
                }
                Err(err) => {
                    last_err = Some(err);
                }
            }
        }

        Err(last_err
            .unwrap_or_else(|| MagError::Generic(format!("failed to fetch {}", fetch.filename))))
    }

    fn refresh_torrent_artifacts(&self, fetch: &FetchResource, dest: &Path) -> MagResult<()> {
        for url in &fetch.urls {
            if let Some(info_hash) = info_hash_from_url(url)? {
                let dir = self.torrent_root.join(&info_hash);
                if self.touch_torrent_dir_path(&dir, dest)? {
                    return Ok(());
                }
            }
        }

        if fetch.urls.is_empty() {
            return Ok(());
        }

        let torrent_info = self.create_torrent_for_file(fetch, dest)?;
        self.write_torrent_artifacts(fetch, dest, &torrent_info)
    }

    fn touch_torrent_dir_path(&self, dir: &Path, source_path: &Path) -> MagResult<bool> {
        if !dir.exists() {
            return Ok(false);
        }

        let torrent_path = dir.join("resource.torrent");
        if !torrent_path.exists() {
            return Ok(false);
        }

        touch_path(&torrent_path)?;

        let TorrentSeedInfo { relative_path, .. } =
            load_torrent_seed_info(&torrent_path).map_err(|err| {
                MagError::Generic(format!(
                    "failed to parse torrent metadata in {}: {err:#}",
                    torrent_path.display()
                ))
            })?;

        let data_path = dir.join(&relative_path);
        if !data_path.exists() {
            copy_file_atomically(source_path, &data_path)?;
        } else {
            touch_path(&data_path)?;
        }

        touch_path(dir)?;
        Ok(true)
    }

    fn fetch_url(
        &self,
        fetch: &FetchResource,
        url: &str,
        dest: &Path,
    ) -> MagResult<DownloadOutcome> {
        if is_torrent_url(url) {
            let fetcher = self.torrent_fetcher()?;
            let tmp_dest = temp_path_for(dest);
            if tmp_dest.exists() {
                match fs::remove_file(&tmp_dest) {
                    Ok(()) => {}
                    Err(err) if err.kind() == ErrorKind::NotFound => {}
                    Err(err) => return Err(err.into()),
                }
            }
            let request = TorrentDownloadRequest {
                url: url.to_string(),
                sha256: fetch.sha256.clone(),
                filename: fetch.filename.clone(),
                dest: tmp_dest.clone(),
            };

            let download = fetcher.download(request)?;

            Ok(DownloadOutcome {
                path: tmp_dest,
                torrent: Some(TorrentInfo {
                    info_hash: download.info_hash,
                    relative_path: download.relative_path,
                    torrent_bytes: download.torrent_bytes,
                }),
            })
        } else {
            let (temp_path, temp_file) = create_temp_file(dest)?;
            let result = if let Ok(parsed) = Url::parse(url) {
                match parsed.scheme() {
                    "file" => {
                        let path = file_url_to_path(&parsed)?;
                        write_stream_with_feedback(File::open(path)?, temp_file, None, None)
                    }
                    "http" | "https" => {
                        let mut response = self.client.get(parsed.clone()).send()?;
                        if !response.status().is_success() {
                            return Err(MagError::Generic(format!(
                                "failed to download {url}: HTTP {}",
                                response.status()
                            )));
                        }
                        let total = response.content_length();
                        write_stream_with_feedback(&mut response, temp_file, Some(url), total)
                    }
                    other => Err(MagError::Generic(format!(
                        "unsupported fetch URL scheme: {other}"
                    ))),
                }
            } else {
                let path = Path::new(url);
                if !path.exists() {
                    return Err(MagError::Generic(format!("fetch source not found: {url}")));
                }
                write_stream_with_feedback(File::open(path)?, temp_file, None, None)
            };

            match result {
                Ok(()) => Ok(DownloadOutcome {
                    path: temp_path,
                    torrent: None,
                }),
                Err(err) => {
                    let _ = fs::remove_file(&temp_path);
                    Err(err)
                }
            }
        }
    }

    fn create_torrent_for_file(
        &self,
        fetch: &FetchResource,
        path: &Path,
    ) -> MagResult<TorrentInfo> {
        let runtime = TokioRuntimeBuilder::new_current_thread()
            .enable_all()
            .build()
            .map_err(|err| MagError::Generic(format!("failed to build tokio runtime: {err}")))?;

        let result = runtime
            .block_on(create_torrent(
                path,
                CreateTorrentOptions {
                    name: Some(&fetch.filename),
                    piece_length: Some(4 * 1024 * 1024),
                },
            ))
            .map_err(|err| {
                MagError::Generic(format!(
                    "failed to create torrent for {}: {err:#}",
                    fetch.filename
                ))
            })?;

        drop(runtime);

        let bytes = result
            .as_bytes()
            .map_err(|err| {
                MagError::Generic(format!(
                    "failed to serialize torrent for {}: {err:#}",
                    fetch.filename
                ))
            })?
            .to_vec();
        let info_hash = info_hash_to_hex(result.info_hash());

        Ok(TorrentInfo {
            info_hash,
            relative_path: PathBuf::from(&fetch.filename),
            torrent_bytes: bytes,
        })
    }

    fn write_torrent_artifacts(
        &self,
        _fetch: &FetchResource,
        data_path: &Path,
        info: &TorrentInfo,
    ) -> MagResult<()> {
        let torrent_dir = self.torrent_root.join(&info.info_hash);
        fs::create_dir_all(&torrent_dir)?;

        let torrent_path = torrent_dir.join("resource.torrent");
        let tmp_torrent = torrent_path.with_extension("tmp");
        {
            let mut file = File::create(&tmp_torrent)?;
            file.write_all(&info.torrent_bytes)?;
            file.sync_all()?;
        }
        fs::rename(&tmp_torrent, &torrent_path)?;
        touch_path(&torrent_path)?;

        let copy_path = torrent_dir.join(&info.relative_path);
        copy_file_atomically(data_path, &copy_path)?;
        touch_path(&torrent_dir)?;
        Ok(())
    }

    pub fn package_artifact_path(&self, package: &Package) -> PathBuf {
        self.store_root
            .join(format!("{}.tar.zst", package_base_name(package)))
    }
}

fn copy_file_atomically(src: &Path, dest: &Path) -> MagResult<()> {
    if let Some(parent) = dest.parent() {
        fs::create_dir_all(parent)?;
    }

    let tmp = dest.with_extension("tmp");
    if let Some(parent) = tmp.parent() {
        fs::create_dir_all(parent)?;
    }

    if let Ok(metadata) = fs::symlink_metadata(&tmp) {
        if metadata.is_dir() {
            fs::remove_dir_all(&tmp)?;
        } else {
            fs::remove_file(&tmp)?;
        }
    }

    fs::copy(src, &tmp)?;
    {
        let file = OpenOptions::new().read(true).write(true).open(&tmp)?;
        file.sync_all()?;
    }

    if let Ok(metadata) = fs::symlink_metadata(dest) {
        if metadata.is_dir() {
            fs::remove_dir_all(dest)?;
        } else {
            fs::remove_file(dest)?;
        }
    }

    fs::rename(&tmp, dest)?;
    touch_path(dest)?;
    Ok(())
}

fn info_hash_from_url(url: &str) -> MagResult<Option<String>> {
    let trimmed = url.trim();
    if !is_torrent_url(trimmed) {
        return Ok(None);
    }

    if trimmed.starts_with("magnet:") || trimmed.len() == 40 {
        let magnet = Magnet::parse(trimmed).map_err(|err| {
            MagError::Generic(format!("failed to parse magnet link {trimmed}: {err:#}"))
        })?;
        if let Some(id20) = magnet.as_id20() {
            return Ok(Some(info_hash_to_hex(id20)));
        }
        if let Some(id32) = magnet.as_id32() {
            return Ok(Some(id32.as_string()));
        }
        return Err(MagError::Generic(format!(
            "magnet link {trimmed} did not contain a supported info hash"
        )));
    }

    Ok(None)
}

fn file_url_to_path(url: &Url) -> MagResult<PathBuf> {
    if url.scheme() != "file" {
        return Err(MagError::Generic(format!("expected file URL, got {}", url)));
    }

    if let Some(host) = url.host_str() {
        if !host.is_empty() && host != "localhost" {
            return Err(MagError::Generic(format!(
                "unsupported file URL host: {host}"
            )));
        }
    }

    let path = url
        .to_file_path()
        .map_err(|_| MagError::Generic(format!("invalid file URL: {url}")))?;

    if !path.is_absolute() {
        return Err(MagError::Generic(
            "file URLs must reference absolute paths".into(),
        ));
    }

    Ok(path)
}

fn run_bwrap_build(package: &Package, rootfs: &Path, parallelism: usize) -> MagResult<()> {
    let script = package.build.as_str();
    if script.is_empty() {
        return Ok(());
    }

    let build_root = rootfs.parent().ok_or_else(|| {
        MagError::Generic("rootfs directory missing parent for build script staging".into())
    })?;
    let script_host_path = build_root.join(format!(
        ".magpkg-build-script-{}-{}",
        package.hash,
        std::process::id()
    ));

    {
        let mut file = File::create(&script_host_path)?;
        file.write_all(script.as_bytes())?;
        if !script.ends_with('\n') {
            file.write_all(b"\n")?;
        }
        file.sync_all()?;
    }
    let mut perms = fs::metadata(&script_host_path)?.permissions();
    perms.set_mode(0o700);
    fs::set_permissions(&script_host_path, perms)?;

    let script_container_path = "/tmp/.magpkg-build-script";

    let mut cmd = Command::new("bwrap");
    cmd.arg("--unshare-net")
        .arg("--bind")
        .arg(rootfs)
        .arg("/")
        .arg("--dev-bind")
        .arg("/dev")
        .arg("/dev")
        .arg("--proc")
        .arg("/proc")
        .arg("--clearenv")
        .arg("--ro-bind")
        .arg(&script_host_path)
        .arg(script_container_path);

    let path_segments = [
        "/usr/bin",
        "/bin",
        "/store/bin",
        "/store/sbin",
        "/usr/sbin",
        "/sbin",
    ];
    let path_value = path_segments.join(":");
    cmd.arg("--setenv").arg("PATH").arg(&path_value);
    cmd.arg("--setenv").arg("SHELL").arg("/bin/sh");
    cmd.arg("--setenv").arg("CONFIG_SHELL").arg("/bin/sh");
    cmd.arg("--setenv")
        .arg("BUILD_PARALLELISM")
        .arg(parallelism.to_string());
    cmd.arg("--setenv").arg("HOME").arg("/build");
    if let Ok(term) = std::env::var("TERM") {
        cmd.arg("--setenv").arg("TERM").arg(term);
    }

    cmd.arg("--chdir").arg("/build");
    cmd.arg("/bin/sh");
    cmd.arg(script_container_path);

    let status = match cmd.status() {
        Ok(status) => status,
        Err(err) => {
            let _ = fs::remove_file(&script_host_path);
            return Err(err.into());
        }
    };
    match fs::remove_file(&script_host_path) {
        Ok(_) => {}
        Err(err) if err.kind() == ErrorKind::NotFound => {}
        Err(err) => return Err(err.into()),
    }

    if !status.success() {
        let code = status.code().unwrap_or(-1);
        return Err(MagError::CommandFailure {
            context: format!("build script for {}", package_base_name(package)),
            status: code,
        });
    }

    Ok(())
}

fn build_via_untar(fetches: &[PathBuf], out_dir: &Path) -> MagResult<()> {
    if fetches.is_empty() {
        return Err(MagError::Generic(
            "untar build script requires at least one fetch resource".into(),
        ));
    }

    clear_directory(out_dir)?;
    for fetch in fetches {
        unpack_fetch_archive(fetch, out_dir)?;
    }
    Ok(())
}

fn pack_output(src: &Path, dest: &Path) -> MagResult<()> {
    if !src.exists() {
        fs::create_dir_all(src)?;
    }
    if let Some(parent) = dest.parent() {
        fs::create_dir_all(parent)?;
    }
    let tmp_tar = dest.with_extension("tmp");
    if tmp_tar.exists() {
        fs::remove_file(&tmp_tar)?;
    }

    let file = File::create(&tmp_tar)?;
    let encoder = ZstdEncoder::new(file, 0)?;
    {
        let mut builder = Builder::new(encoder.auto_finish());
        builder.follow_symlinks(false);
        builder.append_dir_all(".", src)?;
        builder.finish()?;
    }

    if dest.exists() {
        fs::remove_file(dest)?;
    }
    fs::rename(&tmp_tar, dest)?;
    Ok(())
}

fn unpack_fetch_archive(archive_path: &Path, dest: &Path) -> MagResult<()> {
    let file = File::open(archive_path)?;
    match archive_path.extension().and_then(|ext| ext.to_str()) {
        Some("zst") => {
            let decoder = ZstdDecoder::new(file)?;
            let mut archive = tar::Archive::new(decoder);
            archive.unpack(dest)?;
        }
        Some("gz") => {
            let decoder = GzDecoder::new(file);
            let mut archive = tar::Archive::new(decoder);
            archive.unpack(dest)?;
        }
        Some("tar") => {
            let mut archive = tar::Archive::new(file);
            archive.unpack(dest)?;
        }
        _ => {
            return Err(MagError::Generic(format!(
                "unsupported archive format for {}",
                archive_path.display()
            )));
        }
    }
    Ok(())
}

fn extract_tar_zst(archive_path: &Path, dest: &Path) -> MagResult<()> {
    let file = File::open(archive_path)?;
    let decoder = ZstdDecoder::new(file)?;
    let mut archive = tar::Archive::new(decoder);

    let entries = archive.entries().map_err(|err| {
        MagError::Generic(format!(
            "failed to read archive entries from {}: {err}",
            archive_path.display()
        ))
    })?;

    for entry_result in entries {
        let mut entry = entry_result.map_err(|err| {
            MagError::Generic(format!(
                "failed to process entry from {}: {err}",
                archive_path.display()
            ))
        })?;

        let entry_type = entry.header().entry_type();
        let rel_path = entry.path().map_err(|err| {
            MagError::Generic(format!(
                "invalid archive path in {}: {err}",
                archive_path.display()
            ))
        })?;
        let rel_path = rel_path.into_owned();

        prepare_entry_target(dest, &rel_path, entry_type)?;
        entry.unpack_in(dest)?;
    }

    Ok(())
}

fn write_stream_with_feedback<R: Read>(
    mut reader: R,
    mut file: File,
    label: Option<&str>,
    total: Option<u64>,
) -> MagResult<()> {
    let mut buffer = [0u8; 8192];
    let mut transferred: u64 = 0;
    let mut last_report = label.map(|_| Instant::now());

    loop {
        let read = reader.read(&mut buffer)?;
        if read == 0 {
            break;
        }
        transferred += read as u64;
        file.write_all(&buffer[..read])?;

        if let (Some(label), Some(last)) = (label, last_report.as_mut()) {
            if last.elapsed() >= Duration::from_secs(5) {
                print_download_status(label, transferred, total);
                *last = Instant::now();
            }
        }
    }

    file.flush()?;
    file.sync_all()?;

    if let Some(label) = label {
        print_download_complete(label, transferred, total);
    }

    Ok(())
}

fn prepare_entry_target(dest: &Path, rel_path: &Path, entry_type: EntryType) -> io::Result<()> {
    if rel_path.components().next().is_none() {
        return Ok(());
    }

    let target = dest.join(rel_path);
    let metadata = match fs::symlink_metadata(&target) {
        Ok(metadata) => metadata,
        Err(err) if err.kind() == ErrorKind::NotFound => return Ok(()),
        Err(err) => return Err(err),
    };

    let file_type = metadata.file_type();
    match entry_type {
        EntryType::Directory => {
            if file_type.is_dir() {
                return Ok(());
            }
            if file_type.is_symlink() || file_type.is_file() {
                fs::remove_file(&target)
            } else {
                fs::remove_dir_all(&target)
            }
        }
        _ => {
            if file_type.is_dir() {
                fs::remove_dir_all(&target)
            } else {
                fs::remove_file(&target)
            }
        }
    }
}

fn is_torrent_url(url: &str) -> bool {
    if url.trim_start().starts_with("magnet:") {
        return true;
    }

    if let Ok(parsed) = Url::parse(url) {
        if parsed.scheme() == "magnet" {
            return true;
        }
        let path = parsed.path().to_ascii_lowercase();
        if path.ends_with(".torrent") {
            return true;
        }
    }

    false
}

fn info_hash_to_hex(id: Id20) -> String {
    hex::encode(id.0)
}

fn temp_path_for(dest: &Path) -> PathBuf {
    match dest.file_name().and_then(|name| name.to_str()) {
        Some(name) if !name.is_empty() => dest.with_file_name(format!("{name}.tmp")),
        _ => dest.with_file_name("fetch.tmp"),
    }
}

fn create_temp_file(dest: &Path) -> io::Result<(PathBuf, File)> {
    let candidate = temp_path_for(dest);
    if let Some(parent) = candidate.parent() {
        fs::create_dir_all(parent)?;
    }
    if candidate.exists() {
        match fs::remove_file(&candidate) {
            Ok(()) => {}
            Err(err) if err.kind() == ErrorKind::NotFound => {}
            Err(err) => return Err(err),
        }
    }

    match OpenOptions::new()
        .write(true)
        .create_new(true)
        .open(&candidate)
    {
        Ok(file) => Ok((candidate, file)),
        Err(err) => Err(err),
    }
}

fn verify_sha256(path: &Path, expected: &str) -> MagResult<bool> {
    let mut file = File::open(path)?;
    let mut hasher = Sha256::new();
    let mut buffer = [0u8; 8192];
    loop {
        let read = file.read(&mut buffer)?;
        if read == 0 {
            break;
        }
        hasher.update(&buffer[..read]);
    }
    let actual = format!("{:x}", hasher.finalize());
    Ok(actual == expected.trim().to_ascii_lowercase())
}

fn clear_directory(path: &Path) -> io::Result<()> {
    if !path.exists() {
        fs::create_dir_all(path)?;
        return Ok(());
    }

    for entry in fs::read_dir(path)? {
        let entry = entry?;
        let entry_path = entry.path();
        let file_type = entry.file_type()?;
        if file_type.is_dir() {
            fs::remove_dir_all(entry_path)?;
        } else {
            fs::remove_file(entry_path)?;
        }
    }
    Ok(())
}

fn package_base_from_entry(name: &str) -> Option<String> {
    for suffix in [".tar.zst", ".build", ".lock"] {
        if name.ends_with(suffix) {
            return Some(name.trim_end_matches(suffix).to_string());
        }
    }
    None
}

fn remove_path_if_expired(path: &Path, now: SystemTime, expiry: Duration) -> io::Result<bool> {
    let metadata = match fs::metadata(path) {
        Ok(metadata) => metadata,
        Err(err) if err.kind() == ErrorKind::NotFound => return Ok(false),
        Err(err) => return Err(err),
    };

    if !is_metadata_expired(&metadata, now, expiry) {
        return Ok(false);
    }

    if metadata.is_dir() {
        fs::remove_dir_all(path)?;
    } else {
        fs::remove_file(path)?;
    }
    Ok(true)
}

fn is_path_expired(path: &Path, now: SystemTime, expiry: Duration) -> io::Result<bool> {
    let metadata = match fs::metadata(path) {
        Ok(metadata) => metadata,
        Err(err) if err.kind() == ErrorKind::NotFound => return Ok(false),
        Err(err) => return Err(err),
    };
    Ok(is_metadata_expired(&metadata, now, expiry))
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

fn touch_path(path: &Path) -> io::Result<()> {
    if !path.exists() {
        return Ok(());
    }

    let now = FileTime::now();
    match set_file_times(path, now, now) {
        Ok(()) => Ok(()),
        Err(err) if err.kind() == ErrorKind::Unsupported => Ok(()),
        Err(err) if err.kind() == ErrorKind::InvalidInput => Ok(()),
        Err(err) => Err(err),
    }
}

fn print_download_status(label: &str, transferred: u64, total: Option<u64>) {
    match total {
        Some(total) if total > 0 => {
            let percent = (transferred as f64 / total as f64 * 100.0).min(100.0);
            println!(
                "downloading {label}: {} / {} ({percent:.1}%)",
                format_bytes(transferred),
                format_bytes(total)
            );
        }
        _ => println!("downloading {label}: {}", format_bytes(transferred)),
    }
}

fn print_download_complete(label: &str, transferred: u64, total: Option<u64>) {
    match total {
        Some(total) if total > 0 => println!(
            "downloading {label}: complete ({} / {})",
            format_bytes(transferred),
            format_bytes(total)
        ),
        _ => println!(
            "downloading {label}: complete ({})",
            format_bytes(transferred)
        ),
    }
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

fn collect_closure(pkg: Rc<Package>, visited: &mut HashSet<String>, order: &mut Vec<Rc<Package>>) {
    if !visited.insert(pkg.hash.clone()) {
        return;
    }

    for dep in &pkg.run_deps {
        collect_closure(dep.clone(), visited, order);
    }
    for dep in &pkg.build_deps {
        collect_closure(dep.clone(), visited, order);
    }

    order.push(pkg);
}

fn package_base_name(package: &Package) -> String {
    match package.name.as_deref() {
        Some(name) if !name.is_empty() => format!("{name}-{}", package.hash),
        _ => format!("pkg-{}", package.hash),
    }
}
