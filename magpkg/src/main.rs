use std::{
    collections::HashSet,
    fs::{self, File},
    io,
    os::unix::fs::PermissionsExt,
    path::{Path, PathBuf},
    time::Duration,
};

use clap::{Args, Parser, Subcommand};
use jrsonnet_evaluator::error::Error as JrError;
use jrsonnet_evaluator::{State, Val, trace::PathResolver};
use jrsonnet_stdlib::ContextInitializer as StdlibContext;
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
use crate::package::PackageGraphBuilder;
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
    /// Materialize a runtime environment and helper script for an interactive shell.
    Venv(VenvArgs),
}

#[derive(Args)]
struct BuildArgs {
    /// Jsonnet expression to evaluate and convert into packages.
    expression: String,
    /// Parallelism to pass to package build scripts via BUILD_PARALLELISM.
    #[arg(long, default_value_t = default_parallelism())]
    parallelism: usize,
}

#[derive(Args)]
struct FetchArgs {
    /// Jsonnet expression to evaluate and convert into packages.
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
    /// Enable all cleanup categories (packages, fetched, torrents).
    #[arg(long)]
    all: bool,
}

#[derive(Args)]
struct SeedArgs {
    /// Stop seeding torrents whose metadata is older than this many days.
    #[arg(long, default_value_t = 30)]
    max_age_days: u64,
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
    /// Jsonnet expression to evaluate into packages.
    expression: String,
    /// Directory where the virtual environment should be written.
    #[arg(short = 'o', long, value_name = "DIR")]
    output: PathBuf,
    /// Parallelism to pass to package build scripts via BUILD_PARALLELISM.
    #[arg(long, default_value_t = default_parallelism())]
    parallelism: usize,
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

    Ok(())
}

fn run_seed(args: SeedArgs) -> MagResult<()> {
    let store = PackageStore::new()?;
    let seconds_per_day = 24 * 60 * 60;
    let expiry = Duration::from_secs(args.max_age_days.saturating_mul(seconds_per_day));
    let seeder = TorrentSeeder::new(store.torrent_root().to_path_buf(), store.seed_root())?;

    let listen_port = if args.no_listen {
        None
    } else {
        Some(args.listen_port.unwrap_or(DEFAULT_SEED_PORT))
    };

    seeder.run(expiry, listen_port)
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
    let manifest_value = evaluate_expression(&args.expression)?;
    let mut builder = PackageGraphBuilder::default();
    let packages = builder.packages_from_value(manifest_value)?;

    let store = PackageStore::new()?;
    store.build_packages(&packages, args.parallelism)?;

    fs::create_dir_all(&args.output)?;
    let rootfs_dir = args.output.join("rootfs");
    store.export_runtime_closure_rootfs(&packages, &rootfs_dir)?;

    write_venv_run_script(&args.output)?;

    println!(
        "Virtual environment created at {} (use run.sh to enter)",
        args.output.display()
    );

    Ok(())
}

fn write_venv_run_script(dir: &Path) -> MagResult<()> {
    let script_path = dir.join("run.sh");
    let contents = r#"#!/bin/sh
set -euo pipefail

SCRIPT_DIR="$(cd -- "$(dirname -- "$0")" && pwd)"
ROOTFS="$SCRIPT_DIR/rootfs"

if [ ! -d "$ROOTFS" ]; then
    echo "magpkg venv: missing rootfs at $ROOTFS" >&2
    exit 1
fi

HOST_CWD="$(pwd)"
TARGET_DIR="$HOST_CWD"
case "$TARGET_DIR" in
    /home/*|/tmp/*) ;;
    *) TARGET_DIR="/" ;;
esac

if [ "$#" -eq 0 ]; then
    set -- /bin/sh
fi

exec bwrap \
    --bind "$ROOTFS" / \
    --dev-bind /dev /dev \
    --proc /proc \
    --tmpfs /run \
    --bind /home /home \
    --bind /tmp /tmp \
    --setenv PATH "${PATH:-/usr/bin:/bin:/usr/sbin:/sbin}" \
    --setenv HOME "${HOME:-/root}" \
    --chdir "$TARGET_DIR" \
    "$@"
"#;

    fs::write(&script_path, contents)?;
    let mut perms = fs::metadata(&script_path)?.permissions();
    perms.set_mode(0o755);
    fs::set_permissions(&script_path, perms)?;
    Ok(())
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
