use std::{
    collections::{BTreeMap, HashSet},
    env,
    ffi::OsString,
    fs::{self, File, OpenOptions},
    io::{self, Write},
    os::unix::{ffi::OsStrExt, fs::PermissionsExt, fs::symlink, process::ExitStatusExt},
    path::{Path, PathBuf},
    process,
    process::Command,
    rc::Rc,
    time::Duration,
};

use clap::{Args, Parser, Subcommand};
use fs2::FileExt;
use jrsonnet_evaluator::error::Error as JrError;
use jrsonnet_evaluator::{ObjValue, State, Val, trace::PathResolver};
use jrsonnet_stdlib::ContextInitializer as StdlibContext;
use sha2::{Digest, Sha256};
use thiserror::Error;

mod btfetcher;
mod btseed;
mod errors;
mod imports;
mod package;
mod store;

use crate::btseed::TorrentSeeder;
use crate::errors::format_jr_error;
use crate::imports::MagImportResolver;
use crate::package::{Package, PackageGraphBuilder, collect_runtime_closure};
use crate::store::{CleanupOptions, PackageStore};

const DEFAULT_SEED_PORT: u16 = 6881;

fn main() {
    if let Err(err) = try_main() {
        report_error(&err);
        std::process::exit(1);
    }
}

fn try_main() -> MagResult<()> {
    let cli = Cli::parse();
    match cli.command {
        Commands::Build(args) => run_build(args),
        Commands::Fetch(args) => run_fetch(args),
        Commands::Cleanup(args) => run_cleanup(args),
        Commands::Seed(args) => run_seed(args),
        Commands::ExportTarball(args) => run_export_tarball(args),
        Commands::Venv(args) => run_venv(args),
    }
}

#[derive(Parser)]
#[command(
    name = "magpkg",
    version,
    about = "Magnet Linux package manager tooling"
)]
struct Cli {
    #[command(subcommand)]
    command: Commands,
}

#[derive(Subcommand)]
enum Commands {
    /// Evaluate a Jsonnet manifest and build the package graph.
    Build(BuildArgs),
    /// Pre-fetch sources for a package graph without building.
    Fetch(FetchArgs),
    /// Remove cached artifacts older than the expiry window.
    Cleanup(CleanupArgs),
    /// Seed cached torrents so peers can download sources from this machine.
    Seed(SeedArgs),
    /// Export the runtime closure of packages as a tarball.
    ExportTarball(ExportTarballArgs),
    /// Materialize a runtime environment under the store and launch a venv inside it.
    Venv(VenvArgs),
}

#[derive(Args)]
struct BuildArgs {
    /// Jsonnet expression to evaluate and convert into packages.
    #[arg(short = 'e', long = "expression", value_name = "EXPR", required = true)]
    expression: String,
    /// Parallelism to pass to package build scripts via BUILD_PARALLELISM.
    #[arg(long, default_value_t = default_parallelism())]
    parallelism: usize,
}

#[derive(Args)]
struct FetchArgs {
    /// Jsonnet expression to evaluate and convert into packages.
    #[arg(short = 'e', long = "expression", value_name = "EXPR", required = true)]
    expression: String,
    /// Only fetch sources for packages whose artifacts are not yet built.
    #[arg(long)]
    missing_only: bool,
}

#[derive(Args)]
struct CleanupArgs {
    /// Remove store entries older than this many days.
    #[arg(long, default_value_t = 30)]
    max_age_days: u64,
    /// Remove expired package tarballs along with temp build directories.
    #[arg(long)]
    packages: bool,
    /// Remove expired cached fetch payloads (content-addressed files).
    #[arg(long)]
    fetched: bool,
    /// Remove expired torrent payload copies and metadata.
    #[arg(long)]
    torrents: bool,
    /// Remove expired cached virtual environment root filesystems.
    #[arg(long)]
    venvs: bool,
    /// Enable all cleanup categories (packages, fetched, torrents, venvs).
    #[arg(long)]
    all: bool,
}

#[derive(Args)]
struct SeedArgs {
    /// Listen for inbound BitTorrent peers on the given TCP port (default 6881).
    #[arg(long, value_name = "PORT", conflicts_with = "no_listen")]
    listen_port: Option<u16>,
    /// Run the seeder without opening an inbound TCP port.
    #[arg(long, conflicts_with = "listen_port")]
    no_listen: bool,
}

#[derive(Args)]
struct ExportTarballArgs {
    /// Jsonnet expression to evaluate into packages.
    #[arg(short = 'e', long = "expression", value_name = "EXPR", required = true)]
    expression: String,
    /// Write the tarball to this path instead of stdout. Use '-' for stdout.
    #[arg(short, long, value_name = "PATH")]
    output: Option<PathBuf>,
    /// Parallelism to pass to package build scripts via BUILD_PARALLELISM.
    #[arg(long, default_value_t = default_parallelism())]
    parallelism: usize,
}

#[derive(Args)]
struct VenvArgs {
    /// Jsonnet expression describing the virtual environment.
    #[arg(
        short = 'e',
        long = "expression",
        value_name = "EXPR",
        conflicts_with = "file",
        required_unless_present = "file"
    )]
    expression: Option<String>,
    /// Path to a Jsonnet file describing the virtual environment (shorthand for `import`).
    #[arg(
        short = 'f',
        long = "file",
        value_name = "PATH",
        conflicts_with = "expression"
    )]
    file: Option<PathBuf>,
    /// Parallelism to pass to package build scripts via BUILD_PARALLELISM.
    #[arg(long, default_value_t = default_parallelism())]
    parallelism: usize,
    /// Command to run inside the venv (defaults to /bin/sh when omitted).
    #[arg(trailing_var_arg = true, value_name = "COMMAND")]
    command: Vec<String>,
}

#[derive(Debug, Error)]
enum MagError {
    #[error("failed to evaluate expression: {message}")]
    ExpressionEval {
        message: String,
        #[source]
        source: JrError,
    },
    #[error("dependency cycle detected")]
    DependencyCycle,
    #[error("{context}: {message}")]
    Evaluation {
        context: String,
        message: String,
        #[source]
        source: JrError,
    },
    #[error("io error: {source}")]
    Io {
        #[from]
        source: std::io::Error,
    },
    #[error("network error: {source}")]
    Network {
        #[from]
        source: reqwest::Error,
    },
    #[error("{context} failed with status {status}")]
    CommandFailure { context: String, status: i32 },
    #[error("{0}")]
    Generic(String),
}

type MagResult<T> = std::result::Result<T, MagError>;

fn run_build(args: BuildArgs) -> MagResult<()> {
    let manifest_value = evaluate_expression(&args.expression)?;
    let mut builder = PackageGraphBuilder::default();
    let packages = builder.packages_from_value(manifest_value)?;

    let store = PackageStore::new()?;
    store.build_packages(&packages, args.parallelism)?;

    let mut seen = HashSet::new();
    for package in packages {
        if seen.insert(package.hash.clone()) {
            let path = store.package_artifact_path(&package);
            println!("{}", path.display());
        }
    }

    Ok(())
}

fn run_fetch(args: FetchArgs) -> MagResult<()> {
    let manifest_value = evaluate_expression(&args.expression)?;
    let mut builder = PackageGraphBuilder::default();
    let packages = builder.packages_from_value(manifest_value)?;

    let store = PackageStore::new()?;
    store.fetch_packages(&packages, args.missing_only)?;

    Ok(())
}

fn run_cleanup(args: CleanupArgs) -> MagResult<()> {
    let store = PackageStore::new()?;
    let seconds_per_day = 24 * 60 * 60;
    let expiry = Duration::from_secs(args.max_age_days.saturating_mul(seconds_per_day));
    let options = CleanupOptions {
        packages: args.all || args.packages,
        fetched: args.all || args.fetched,
        torrents: args.all || args.torrents,
        venvs: args.all || args.venvs,
    };
    let stats = store.cleanup(expiry, options)?;

    println!("Cleanup completed (max age: {} day(s)).", args.max_age_days);

    if stats.package_artifacts_removed
        + stats.package_build_dirs_removed
        + stats.package_lock_files_removed
        > 0
    {
        println!(
            "  Package artifacts removed: {}, build dirs: {}, lock files: {}",
            stats.package_artifacts_removed,
            stats.package_build_dirs_removed,
            stats.package_lock_files_removed
        );
    }

    if stats.fetch_files_removed + stats.fetch_partials_removed + stats.fetch_lock_files_removed > 0
    {
        println!(
            "  Fetch files removed: {}, partials: {}, lock files: {}",
            stats.fetch_files_removed, stats.fetch_partials_removed, stats.fetch_lock_files_removed
        );
    }

    if stats.venv_rootfs_removed > 0 {
        println!("  Venv rootfs removed: {}", stats.venv_rootfs_removed);
    }

    Ok(())
}

fn run_seed(args: SeedArgs) -> MagResult<()> {
    let store = PackageStore::new()?;
    let seeder = TorrentSeeder::new(store.torrent_root().to_path_buf())?;

    let listen_port = if args.no_listen {
        None
    } else {
        Some(args.listen_port.unwrap_or(DEFAULT_SEED_PORT))
    };

    seeder.run(listen_port)
}

fn run_export_tarball(args: ExportTarballArgs) -> MagResult<()> {
    let manifest_value = evaluate_expression(&args.expression)?;
    let mut builder = PackageGraphBuilder::default();
    let packages = builder.packages_from_value(manifest_value)?;

    let store = PackageStore::new()?;
    store.build_packages(&packages, args.parallelism)?;

    match args.output {
        Some(ref path) if path == Path::new("-") => {
            let stdout = io::stdout();
            let mut handle = stdout.lock();
            store.export_runtime_closure_tarball(&packages, &mut handle)?;
        }
        Some(path) => {
            if let Some(parent) = path.parent() {
                if !parent.as_os_str().is_empty() {
                    std::fs::create_dir_all(parent)?;
                }
            }
            let file = File::create(&path)?;
            let mut writer = io::BufWriter::new(file);
            store.export_runtime_closure_tarball(&packages, &mut writer)?;
        }
        None => {
            let stdout = io::stdout();
            let mut handle = stdout.lock();
            store.export_runtime_closure_tarball(&packages, &mut handle)?;
        }
    }

    Ok(())
}

fn run_venv(args: VenvArgs) -> MagResult<()> {
    let VenvArgs {
        expression,
        file,
        parallelism,
        command,
    } = args;

    let manifest_expr = match (expression, file) {
        (Some(expr), None) => expr,
        (None, Some(path)) => format!("import {}", quote_jsonnet_string(&path)?),
        (Some(_), Some(_)) => unreachable!("clap enforces mutual exclusivity"),
        (None, None) => unreachable!("clap enforces presence of expression or file"),
    };

    let manifest_value = evaluate_expression(&manifest_expr)?;
    let mut builder = PackageGraphBuilder::default();
    let spec = VenvSpec::from_value(manifest_value, &mut builder)?;

    let store = PackageStore::new()?;
    store.build_packages(&spec.packages, parallelism)?;

    let rootfs_dir = store.venv_rootfs_dir(&spec.rootfs_hash);
    let rootfs_path = rootfs_dir.join("rootfs");

    if !rootfs_path.exists() {
        fs::create_dir_all(&rootfs_dir)?;
        if let Err(err) = store.export_runtime_closure_rootfs(&spec.packages, &rootfs_path) {
            let _ = fs::remove_dir_all(&rootfs_dir);
            return Err(err);
        }
        if let Err(err) = apply_fs_entries(&rootfs_path, &spec.fs_entries) {
            let _ = fs::remove_dir_all(&rootfs_dir);
            return Err(err);
        }
        println!(
            "Venv rootfs hash {} stored at {}",
            spec.rootfs_hash,
            rootfs_dir.display()
        );
    }

    let command = if command.is_empty() {
        vec![OsString::from("/bin/sh")]
    } else {
        command.iter().map(OsString::from).collect()
    };

    launch_venv(&rootfs_path, &spec, command)
}

fn quote_jsonnet_string(path: &Path) -> MagResult<String> {
    let path_str = path.to_str().ok_or_else(|| {
        MagError::Generic(format!(
            "manifest file path is not valid UTF-8: {}",
            path.display()
        ))
    })?;

    let mut out = String::with_capacity(path_str.len() + 2);
    use std::fmt::Write as _;
    out.push('"');
    for ch in path_str.chars() {
        match ch {
            '\\' => out.push_str("\\\\"),
            '"' => out.push_str("\\\""),
            '\n' => out.push_str("\\n"),
            '\r' => out.push_str("\\r"),
            '\t' => out.push_str("\\t"),
            ch if ch.is_control() => {
                write!(&mut out, "\\u{:04x}", ch as u32).unwrap();
            }
            ch => out.push(ch),
        }
    }
    out.push('"');

    Ok(out)
}

fn launch_venv(rootfs: &Path, spec: &VenvSpec, command: Vec<OsString>) -> MagResult<()> {
    if !rootfs.exists() {
        return Err(MagError::Generic(format!(
            "venv rootfs missing at {}",
            rootfs.display()
        )));
    }

    let lock_path = rootfs.join(".lock");
    let lock_file = File::create(&lock_path)?;
    FileExt::lock_shared(&lock_file)?;

    let host_cwd = env::current_dir()?;
    let mut target_dir = host_cwd.clone();
    if !(target_dir.starts_with("/home") || target_dir.starts_with("/tmp")) {
        target_dir = PathBuf::from("/");
    }

    let mut variables: BTreeMap<String, String> = BTreeMap::new();

    for key in spec.env_keep.iter().cloned() {
        if let Ok(value) = env::var(&key) {
            variables.insert(key, value);
        }
    }

    for (key, value) in &spec.env_set {
        variables.insert(key.clone(), value.clone());
    }

    if !variables.contains_key("PATH") {
        variables.insert(
            "PATH".to_string(),
            "/usr/bin:/bin:/usr/sbin:/sbin".to_string(),
        );
    }

    if !variables.contains_key("LD_LIBRARY_PATH") {
        variables.insert(
            "LD_LIBRARY_PATH".to_string(),
            "/usr/lib64:/usr/lib:/lib".to_string(),
        );
    }

    variables
        .entry("HOME".to_string())
        .or_insert_with(|| env::var("HOME").unwrap_or_else(|_| "/root".into()));

    let mut cmd = Command::new("bwrap");
    cmd.arg("--ro-bind").arg(rootfs).arg("/");

    let mut mounts = Vec::new();
    if spec.use_default_mounts {
        mounts.extend(default_mounts());
    }
    mounts.extend(spec.mounts.clone());

    if !mounts.iter().any(|m| m.target == Path::new("/tmp")) {
        mounts.push(mount_spec(MountKind::Tmpfs, None, "/tmp", false));
    }

    for mount in &mounts {
        match mount.kind {
            MountKind::Bind => {
                let source = mount
                    .source
                    .as_ref()
                    .expect("bind mount requires source path");
                let metadata = match fs::metadata(source) {
                    Ok(meta) => meta,
                    Err(err) if err.kind() == io::ErrorKind::NotFound && mount.optional => {
                        continue;
                    }
                    Err(err) if err.kind() == io::ErrorKind::NotFound => {
                        return Err(MagError::Generic(format!(
                            "bind mount source missing: {}",
                            source.display()
                        )));
                    }
                    Err(err) => return Err(err.into()),
                };
                ensure_mount_target(rootfs, mount, Some(&metadata))?;
                cmd.arg("--bind").arg(source).arg(&mount.target);
            }
            MountKind::RoBind => {
                let source = mount
                    .source
                    .as_ref()
                    .expect("ro-bind mount requires source path");
                let metadata = match fs::metadata(source) {
                    Ok(meta) => meta,
                    Err(err) if err.kind() == io::ErrorKind::NotFound && mount.optional => {
                        continue;
                    }
                    Err(err) if err.kind() == io::ErrorKind::NotFound => {
                        return Err(MagError::Generic(format!(
                            "ro-bind mount source missing: {}",
                            source.display()
                        )));
                    }
                    Err(err) => return Err(err.into()),
                };
                ensure_mount_target(rootfs, mount, Some(&metadata))?;
                cmd.arg("--ro-bind").arg(source).arg(&mount.target);
            }
            MountKind::DevBind => {
                let source = mount
                    .source
                    .as_ref()
                    .expect("dev-bind mount requires source path");
                let metadata = match fs::metadata(source) {
                    Ok(meta) => meta,
                    Err(err) if err.kind() == io::ErrorKind::NotFound && mount.optional => {
                        continue;
                    }
                    Err(err) if err.kind() == io::ErrorKind::NotFound => {
                        return Err(MagError::Generic(format!(
                            "dev-bind mount source missing: {}",
                            source.display()
                        )));
                    }
                    Err(err) => return Err(err.into()),
                };
                ensure_mount_target(rootfs, mount, Some(&metadata))?;
                cmd.arg("--dev-bind").arg(source).arg(&mount.target);
            }
            MountKind::Proc => {
                ensure_mount_target(rootfs, mount, None)?;
                cmd.arg("--proc").arg(&mount.target);
            }
            MountKind::Tmpfs => {
                ensure_mount_target(rootfs, mount, None)?;
                cmd.arg("--tmpfs").arg(&mount.target);
            }
        }
    }

    cmd.arg("--chdir").arg(&target_dir);

    for (key, value) in variables {
        cmd.arg("--setenv").arg(&key).arg(&value);
    }

    cmd.args(command);

    let status = cmd.status();

    drop(lock_file);

    let status = status?;

    if let Some(code) = status.code() {
        if code == 0 {
            Ok(())
        } else {
            process::exit(code);
        }
    } else if let Some(signal) = status.signal() {
        process::exit(128 + signal);
    } else {
        Err(MagError::Generic(
            "bubblewrap exited without providing a status".into(),
        ))
    }
}

struct VenvSpec {
    packages: Vec<Rc<Package>>,
    env_keep: Vec<String>,
    env_set: BTreeMap<String, String>,
    use_default_mounts: bool,
    mounts: Vec<MountSpec>,
    fs_entries: Vec<FsEntry>,
    rootfs_hash: String,
}

#[derive(Debug, Clone)]
struct MountSpec {
    kind: MountKind,
    source: Option<PathBuf>,
    target: PathBuf,
    optional: bool,
}

#[derive(Debug, Clone, Copy)]
enum MountKind {
    Bind,
    RoBind,
    DevBind,
    Proc,
    Tmpfs,
}

#[derive(Debug, Clone)]
struct FsEntry {
    kind: FsEntryKind,
    path: PathBuf,
    mode: Option<u32>,
    contents: Option<Vec<u8>>,
    target: Option<PathBuf>,
}

#[derive(Debug, Clone, Copy, PartialEq, Eq)]
enum FsEntryKind {
    Dir,
    File,
    Symlink,
}

fn ensure_mount_target(
    rootfs: &Path,
    mount: &MountSpec,
    source_meta: Option<&fs::Metadata>,
) -> MagResult<()> {
    if mount.target == Path::new("/") {
        return Ok(());
    }

    let relative = mount.target.strip_prefix("/").unwrap_or(&mount.target);
    let target_path = rootfs.join(relative);

    match mount.kind {
        MountKind::Proc | MountKind::Tmpfs => {
            fs::create_dir_all(&target_path)?;
        }
        MountKind::Bind | MountKind::RoBind | MountKind::DevBind => {
            if let Some(meta) = source_meta {
                if meta.is_dir() {
                    fs::create_dir_all(&target_path)?;
                } else {
                    if let Some(parent) = target_path.parent() {
                        fs::create_dir_all(parent)?;
                    }
                    if !target_path.exists() {
                        File::create(&target_path)?;
                    }
                }
            } else {
                if let Some(parent) = target_path.parent() {
                    fs::create_dir_all(parent)?;
                }
                if !target_path.exists() {
                    File::create(&target_path)?;
                }
            }
        }
    }

    Ok(())
}

fn apply_fs_entries(rootfs: &Path, entries: &[FsEntry]) -> MagResult<()> {
    for entry in entries {
        let rel = entry.path.strip_prefix("/").unwrap_or(&entry.path);
        let abs_path = rootfs.join(rel);

        match entry.kind {
            FsEntryKind::Dir => {
                fs::create_dir_all(&abs_path)?;
                if let Some(mode) = entry.mode {
                    let perms = fs::Permissions::from_mode(mode);
                    fs::set_permissions(&abs_path, perms)?;
                }
            }
            FsEntryKind::File => {
                if let Some(parent) = abs_path.parent() {
                    fs::create_dir_all(parent)?;
                }
                let mut file = OpenOptions::new()
                    .create(true)
                    .truncate(true)
                    .write(true)
                    .open(&abs_path)?;
                if let Some(data) = &entry.contents {
                    file.write_all(data)?;
                }
                file.flush()?;
                if let Some(mode) = entry.mode {
                    let perms = fs::Permissions::from_mode(mode);
                    fs::set_permissions(&abs_path, perms)?;
                }
            }
            FsEntryKind::Symlink => {
                let target = entry.target.as_ref().ok_or_else(|| {
                    MagError::Generic(format!(
                        "symlink entry missing target for {}",
                        entry.path.display()
                    ))
                })?;
                if let Some(parent) = abs_path.parent() {
                    fs::create_dir_all(parent)?;
                }
                if let Err(err) = fs::remove_file(&abs_path) {
                    if err.kind() != io::ErrorKind::NotFound {
                        let _ = fs::remove_dir_all(&abs_path);
                    }
                }
                symlink(target, &abs_path)?;
            }
        }
    }
    Ok(())
}

fn default_mounts() -> Vec<MountSpec> {
    vec![
        mount_spec(MountKind::DevBind, Some("/dev"), "/dev", false),
        mount_spec(MountKind::Proc, None, "/proc", false),
        mount_spec(MountKind::RoBind, Some("/sys"), "/sys", false),
        mount_spec(
            MountKind::RoBind,
            Some("/etc/resolv.conf"),
            "/etc/resolv.conf",
            false,
        ),
        mount_spec(MountKind::RoBind, Some("/etc/hosts"), "/etc/hosts", false),
        mount_spec(MountKind::Bind, Some("/tmp"), "/tmp", false),
    ]
}

fn mount_spec(kind: MountKind, source: Option<&str>, target: &str, optional: bool) -> MountSpec {
    MountSpec {
        kind,
        source: source.map(PathBuf::from),
        target: PathBuf::from(target),
        optional,
    }
}

impl VenvSpec {
    fn from_value(value: Val, builder: &mut PackageGraphBuilder) -> MagResult<Self> {
        let obj = value
            .as_obj()
            .ok_or_else(|| MagError::Generic("venv manifest must evaluate to an object".into()))?;

        let packages_value = get_manifest_field(&obj, "packages")?.ok_or_else(|| {
            MagError::Generic("venv manifest must define a 'packages' field".into())
        })?;
        let packages = builder.packages_from_value(packages_value)?;
        if packages.is_empty() {
            return Err(MagError::Generic(
                "venv manifest field 'packages' must not be empty".into(),
            ));
        }

        let env_keep = read_string_array(&obj, "envKeep")?;
        let env_set = read_string_map(&obj, "envSet")?;
        let use_default_mounts =
            read_optional_bool_field(&obj, "mountDefaults", "venv")?.unwrap_or(true);
        let mounts = read_mounts(&obj)?;
        let fs_entries = read_filesystem_entries(&obj)?;

        let closure = compute_runtime_closure(&packages);
        let rootfs_hash = compute_rootfs_hash(&closure, &fs_entries);

        Ok(Self {
            packages,
            env_keep,
            env_set,
            use_default_mounts,
            mounts,
            fs_entries,
            rootfs_hash,
        })
    }
}

fn get_manifest_field(obj: &ObjValue, field: &str) -> MagResult<Option<Val>> {
    obj.get(field.into()).map_err(|err| {
        let message = format_jr_error(&err);
        MagError::Evaluation {
            context: format!("failed to read field '{field}'"),
            message,
            source: err,
        }
    })
}

fn read_string_array(obj: &ObjValue, field: &str) -> MagResult<Vec<String>> {
    let Some(value) = get_manifest_field(obj, field)? else {
        return Ok(Vec::new());
    };

    match value {
        Val::Null => Ok(Vec::new()),
        Val::Arr(arr) => {
            let mut out = Vec::with_capacity(arr.len());
            for (index, item) in arr.iter().enumerate() {
                let val = item.map_err(|err| {
                    let message = format_jr_error(&err);
                    MagError::Evaluation {
                        context: format!("failed to evaluate element {index} in field '{field}'"),
                        message,
                        source: err,
                    }
                })?;
                match val {
                    Val::Str(s) => out.push(s.to_string()),
                    other => {
                        return Err(MagError::Generic(format!(
                            "field '{field}' must be an array of strings, got {:?}",
                            other.value_type()
                        )));
                    }
                }
            }
            Ok(out)
        }
        other => Err(MagError::Generic(format!(
            "field '{field}' must be an array of strings, got {:?}",
            other.value_type()
        ))),
    }
}

fn read_string_map(obj: &ObjValue, field: &str) -> MagResult<BTreeMap<String, String>> {
    let Some(value) = get_manifest_field(obj, field)? else {
        return Ok(BTreeMap::new());
    };

    match value {
        Val::Null => Ok(BTreeMap::new()),
        Val::Obj(map_obj) => {
            let mut out = BTreeMap::new();
            for key in map_obj.fields() {
                let key_string = key.to_string();
                let entry = map_obj.get(key.clone()).map_err(|err| {
                    let message = format_jr_error(&err);
                    MagError::Evaluation {
                        context: format!("failed to evaluate field '{field}[{key_string}]'"),
                        message,
                        source: err,
                    }
                })?;
                let value = entry.expect("field exists");
                match value {
                    Val::Str(s) => {
                        out.insert(key_string, s.to_string());
                    }
                    other => {
                        return Err(MagError::Generic(format!(
                            "field '{field}' must map to strings, key '{key_string}' has {:?}",
                            other.value_type()
                        )));
                    }
                }
            }
            Ok(out)
        }
        other => Err(MagError::Generic(format!(
            "field '{field}' must be an object mapping to strings, got {:?}",
            other.value_type()
        ))),
    }
}

fn read_required_string_field(obj: &ObjValue, field: &str, context: &str) -> MagResult<String> {
    let value = get_manifest_field(obj, field)?;

    match value {
        Some(Val::Str(s)) => Ok(s.to_string()),
        None | Some(Val::Null) => Err(MagError::Generic(format!(
            "{context}: missing required field '{field}'"
        ))),
        Some(other) => Err(MagError::Generic(format!(
            "{context}: expected field '{field}' to be a string, got {:?}",
            other.value_type()
        ))),
    }
}

fn read_optional_bool_field(obj: &ObjValue, field: &str, context: &str) -> MagResult<Option<bool>> {
    let value = get_manifest_field(obj, field)?;

    match value {
        None | Some(Val::Null) => Ok(None),
        Some(Val::Bool(b)) => Ok(Some(b)),
        Some(other) => Err(MagError::Generic(format!(
            "{context}: expected field '{field}' to be a boolean, got {:?}",
            other.value_type()
        ))),
    }
}

fn read_mounts(obj: &ObjValue) -> MagResult<Vec<MountSpec>> {
    let Some(value) = get_manifest_field(obj, "mounts")? else {
        return Ok(Vec::new());
    };

    match value {
        Val::Null => Ok(Vec::new()),
        Val::Arr(arr) => {
            let mut mounts = Vec::with_capacity(arr.len());
            for (index, item) in arr.iter().enumerate() {
                let context = format!("mounts[{index}]");
                let val = item.map_err(|err| {
                    let message = format_jr_error(&err);
                    MagError::Evaluation {
                        context: format!("failed to evaluate {context}"),
                        message,
                        source: err,
                    }
                })?;
                if let Some(path) = val.as_str() {
                    let path = PathBuf::from(path.to_string());
                    if !path.is_absolute() {
                        return Err(MagError::Generic(format!(
                            "{context}: shorthand mounts must use absolute paths"
                        )));
                    }
                    mounts.push(MountSpec {
                        kind: MountKind::Bind,
                        source: Some(path.clone()),
                        target: path,
                        optional: false,
                    });
                    continue;
                }

                let mount_obj = val.as_obj().ok_or_else(|| {
                    MagError::Generic(format!(
                        "{context} must be an object or string, got {:?}",
                        val.value_type()
                    ))
                })?;

                let mount_type = read_required_string_field(&mount_obj, "type", &context)?;
                let optional =
                    read_optional_bool_field(&mount_obj, "optional", &context)?.unwrap_or(false);
                let kind = match mount_type.as_str() {
                    "bind" => MountKind::Bind,
                    "ro-bind" => MountKind::RoBind,
                    "dev-bind" => MountKind::DevBind,
                    "proc" => MountKind::Proc,
                    "tmpfs" => MountKind::Tmpfs,
                    other => {
                        return Err(MagError::Generic(format!(
                            "{context}: unsupported mount type '{other}'"
                        )));
                    }
                };

                let target_str = read_required_string_field(&mount_obj, "target", &context)?;
                let target = PathBuf::from(target_str);

                let source = match kind {
                    MountKind::Bind | MountKind::RoBind | MountKind::DevBind => {
                        let source_str =
                            read_required_string_field(&mount_obj, "source", &context)?;
                        Some(PathBuf::from(source_str))
                    }
                    MountKind::Proc | MountKind::Tmpfs => None,
                };

                mounts.push(MountSpec {
                    kind,
                    source,
                    target,
                    optional,
                });
            }
            Ok(mounts)
        }
        other => Err(MagError::Generic(format!(
            "field 'mounts' must be an array of objects, got {:?}",
            other.value_type()
        ))),
    }
}

fn read_filesystem_entries(obj: &ObjValue) -> MagResult<Vec<FsEntry>> {
    let Some(value) = get_manifest_field(obj, "fsEntries")? else {
        return Ok(Vec::new());
    };

    match value {
        Val::Null => Ok(Vec::new()),
        Val::Arr(arr) => {
            let mut entries = Vec::with_capacity(arr.len());
            for (index, item) in arr.iter().enumerate() {
                let context = format!("fsEntries[{index}]");
                let val = item.map_err(|err| {
                    let message = format_jr_error(&err);
                    MagError::Evaluation {
                        context: format!("failed to evaluate {context}"),
                        message,
                        source: err,
                    }
                })?;
                let entry_obj = val.as_obj().ok_or_else(|| {
                    MagError::Generic(format!(
                        "{context} must be an object, got {:?}",
                        val.value_type()
                    ))
                })?;

                let entry_type = read_required_string_field(&entry_obj, "type", &context)?;
                let path_str = read_required_string_field(&entry_obj, "path", &context)?;
                let path = PathBuf::from(&path_str);
                if !path.is_absolute() {
                    return Err(MagError::Generic(format!(
                        "{context}: path must be absolute, got {}",
                        path_str
                    )));
                }

                let mode = match entry_obj.get("mode".into()).map_err(|err| {
                    let message = format_jr_error(&err);
                    MagError::Evaluation {
                        context: format!("{context}: failed to read mode"),
                        message,
                        source: err,
                    }
                })? {
                    Some(Val::Null) | None => None,
                    Some(Val::Num(_)) => {
                        return Err(MagError::Generic(format!(
                            "{context}: mode must be provided as a string (e.g. \"0755\")"
                        )));
                    }
                    Some(Val::Str(s)) => {
                        let trimmed = s.to_string();
                        let trimmed = trimmed.trim();
                        let parsed = if trimmed.starts_with("0o") || trimmed.starts_with("0O") {
                            u32::from_str_radix(&trimmed[2..], 8)
                        } else if trimmed.starts_with('0') {
                            u32::from_str_radix(trimmed, 8)
                        } else {
                            trimmed.parse::<u32>()
                        };
                        match parsed {
                            Ok(val) => Some(val),
                            Err(_) => {
                                return Err(MagError::Generic(format!(
                                    "{context}: mode must be a valid integer, got {}",
                                    trimmed
                                )));
                            }
                        }
                    }
                    Some(other) => {
                        return Err(MagError::Generic(format!(
                            "{context}: mode must be a number, got {:?}",
                            other.value_type()
                        )));
                    }
                };

                let (kind, contents, target) = match entry_type.as_str() {
                    "dir" => (FsEntryKind::Dir, None, None),
                    "file" => {
                        let data = match entry_obj.get("contents".into()).map_err(|err| {
                            let message = format_jr_error(&err);
                            MagError::Evaluation {
                                context: format!("{context}: failed to read contents"),
                                message,
                                source: err,
                            }
                        })? {
                            None | Some(Val::Null) => None,
                            Some(Val::Str(s)) => Some(s.to_string().into_bytes()),
                            Some(other) => {
                                return Err(MagError::Generic(format!(
                                    "{context}: file contents must be a string, got {:?}",
                                    other.value_type()
                                )));
                            }
                        };
                        (FsEntryKind::File, data, None)
                    }
                    "symlink" => {
                        let target = read_required_string_field(&entry_obj, "target", &context)?;
                        (FsEntryKind::Symlink, None, Some(PathBuf::from(target)))
                    }
                    other => {
                        return Err(MagError::Generic(format!(
                            "{context}: unsupported fs entry type '{other}'"
                        )));
                    }
                };

                entries.push(FsEntry {
                    kind,
                    path,
                    mode,
                    contents,
                    target,
                });
            }
            Ok(entries)
        }
        other => Err(MagError::Generic(format!(
            "field 'fsEntries' must be an array of objects, got {:?}",
            other.value_type()
        ))),
    }
}

fn compute_runtime_closure(packages: &[Rc<Package>]) -> Vec<Rc<Package>> {
    let mut visited = HashSet::new();
    let mut order = Vec::new();
    for pkg in packages {
        collect_runtime_closure(pkg.clone(), &mut visited, &mut order);
    }
    order
}

fn compute_rootfs_hash(packages: &[Rc<Package>], fs_entries: &[FsEntry]) -> String {
    let mut hasher = Sha256::new();

    let mut package_hashes: Vec<&str> = packages.iter().map(|pkg| pkg.hash.as_str()).collect();
    package_hashes.sort_unstable();
    package_hashes.dedup();
    for hash in package_hashes {
        hasher.update(hash.as_bytes());
        hasher.update(&[0]);
    }

    let mut entries: Vec<&FsEntry> = fs_entries.iter().collect();
    entries.sort_by(|a, b| a.path.cmp(&b.path));
    for entry in entries {
        hasher.update(match entry.kind {
            FsEntryKind::Dir => b"dir" as &[u8],
            FsEntryKind::File => b"file",
            FsEntryKind::Symlink => b"symlink",
        });
        hasher.update(&[0]);
        hasher.update(entry.path.as_os_str().as_bytes());
        hasher.update(&[0]);
        if let Some(mode) = entry.mode {
            hasher.update(&mode.to_be_bytes());
        }
        hasher.update(&[0]);
        match entry.kind {
            FsEntryKind::File => {
                if let Some(contents) = &entry.contents {
                    hasher.update(contents);
                }
            }
            FsEntryKind::Symlink => {
                if let Some(target) = &entry.target {
                    hasher.update(target.as_os_str().as_bytes());
                }
            }
            FsEntryKind::Dir => {}
        }
        hasher.update(&[0xff]);
    }

    hex::encode(hasher.finalize())
}

fn report_error(err: &MagError) {
    eprintln!("Error: {}", err);
}

fn evaluate_expression(expression: &str) -> MagResult<Val> {
    let mut builder = State::builder();
    builder.import_resolver(MagImportResolver::new(Vec::new()));
    builder.context_initializer(StdlibContext::new(PathResolver::new_cwd_fallback()));
    let state = builder.build();

    state.evaluate_snippet("<cli>", expression).map_err(|err| {
        let message = format_jr_error(&err);
        MagError::ExpressionEval {
            message,
            source: err,
        }
    })
}

fn default_parallelism() -> usize {
    std::cmp::max(1, num_cpus::get())
}
