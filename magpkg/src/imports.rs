use std::{
    any::Any,
    fmt,
    hash::{Hash, Hasher},
    path::{Path, PathBuf},
};

use jrsonnet_evaluator::{
    FileImportResolver, ImportResolver,
    error::{ErrorKind, Result as JrResult},
    parser::{SourcePath, SourcePathT},
};
use jrsonnet_gcmodule::{Trace, Tracer};
use reqwest::Url;
use reqwest::blocking::{Client, ClientBuilder};

const USER_AGENT: &str = concat!("magpkg/", env!("CARGO_PKG_VERSION"));

pub struct MagImportResolver {
    file: FileImportResolver,
    client: Client,
}

impl MagImportResolver {
    pub fn new(library_paths: Vec<PathBuf>) -> Self {
        let file = FileImportResolver::new(library_paths);
        let client = ClientBuilder::new()
            .user_agent(USER_AGENT)
            .build()
            .expect("failed to build http client");
        Self { file, client }
    }
}

impl Trace for MagImportResolver {
    fn trace(&self, _tracer: &mut Tracer<'_>) {}

    fn is_type_tracked() -> bool
    where
        Self: Sized,
    {
        false
    }
}

impl ImportResolver for MagImportResolver {
    fn resolve_from(&self, from: &SourcePath, path: &str) -> JrResult<SourcePath> {
        if is_remote_url(path) {
            return Ok(SourcePath::new(RemoteSource::new(path.to_owned())));
        }

        if let Some(base) = from.downcast_ref::<RemoteSource>() {
            let joined = join_remote_url(base.url(), path)?;
            return Ok(SourcePath::new(RemoteSource::new(joined)));
        }

        self.file.resolve_from(from, path)
    }

    fn resolve(&self, path: &Path) -> JrResult<SourcePath> {
        self.file.resolve(path)
    }

    fn load_file_contents(&self, resolved: &SourcePath) -> JrResult<Vec<u8>> {
        if let Some(remote) = resolved.downcast_ref::<RemoteSource>() {
            let response = self
                .client
                .get(remote.url())
                .send()
                .map_err(|err| ErrorKind::ImportIo(err.to_string()))?;

            if !response.status().is_success() {
                return Err(ErrorKind::ImportIo(format!(
                    "HTTP {} fetching {}",
                    response.status(),
                    remote.url()
                ))
                .into());
            }

            let bytes = response
                .bytes()
                .map_err(|err| ErrorKind::ImportIo(err.to_string()))?;
            return Ok(bytes.to_vec());
        }

        self.file.load_file_contents(resolved)
    }

    fn as_any(&self) -> &dyn Any {
        self
    }

    fn as_any_mut(&mut self) -> &mut dyn Any {
        self
    }
}

#[derive(Clone, Hash, PartialEq, Eq)]
struct RemoteSource {
    url: String,
}

impl RemoteSource {
    fn new(url: String) -> Self {
        Self { url }
    }

    fn url(&self) -> &str {
        &self.url
    }
}

impl fmt::Debug for RemoteSource {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        write!(f, "HttpSource({})", self.url)
    }
}

impl fmt::Display for RemoteSource {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        write!(f, "{}", self.url)
    }
}

impl Trace for RemoteSource {
    fn trace(&self, _tracer: &mut Tracer<'_>) {}

    fn is_type_tracked() -> bool
    where
        Self: Sized,
    {
        false
    }
}

impl SourcePathT for RemoteSource {
    fn is_default(&self) -> bool {
        false
    }

    fn path(&self) -> Option<&Path> {
        None
    }

    fn as_any(&self) -> &dyn Any {
        self
    }

    fn dyn_hash(&self, state: &mut dyn Hasher) {
        state.write(self.url.as_bytes());
    }

    fn dyn_eq(&self, other: &dyn SourcePathT) -> bool {
        other
            .as_any()
            .downcast_ref::<Self>()
            .map_or(false, |o| o == self)
    }

    fn dyn_debug(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        fmt::Debug::fmt(self, f)
    }
}

fn is_remote_url(path: &str) -> bool {
    path.starts_with("http://") || path.starts_with("https://")
}

fn join_remote_url(base: &str, path: &str) -> JrResult<String> {
    let base = Url::parse(base)
        .map_err(|err| ErrorKind::ImportIo(format!("invalid base url {base}: {err}")))?;
    let joined = base
        .join(path)
        .map_err(|err| ErrorKind::ImportIo(format!("failed to join {path} onto {base}: {err}")))?;
    Ok(joined.into())
}
