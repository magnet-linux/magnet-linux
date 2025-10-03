use std::{
    collections::{HashMap, HashSet},
    rc::Rc,
};

use jrsonnet_evaluator::{ObjValue, Val};
use sha2::{Digest, Sha256};

use crate::{MagError, MagResult, errors::format_jr_error};

#[derive(Debug)]
pub struct Package {
    pub name: Option<String>,
    pub build: String,
    pub hash: String,
    pub run_deps: Vec<Rc<Package>>,
    pub build_deps: Vec<Rc<Package>>,
    pub fetch: Vec<FetchResource>,
}

#[derive(Debug, Clone)]
pub struct FetchResource {
    pub filename: String,
    pub sha256: String,
    pub urls: Vec<String>,
}

#[derive(Default)]
pub struct PackageGraphBuilder {
    by_obj: HashMap<ObjKey, Rc<Package>>,
    by_hash: HashMap<String, Rc<Package>>,
}

impl PackageGraphBuilder {
    pub fn packages_from_value(&mut self, value: Val) -> MagResult<Vec<Rc<Package>>> {
        match value {
            Val::Arr(arr) => {
                let mut packages = Vec::with_capacity(arr.len());
                for (index, item) in arr.iter().enumerate() {
                    let value = item.map_err(|err| {
                        let message = format_jr_error(&err);
                        MagError::Evaluation {
                            context: format!("failed to evaluate package at index {index}"),
                            message,
                            source: err,
                        }
                    })?;
                    packages.push(self.add_package(value)?);
                }
                Ok(packages)
            }
            other => Ok(vec![self.add_package(other)?]),
        }
    }

    fn add_package(&mut self, value: Val) -> MagResult<Rc<Package>> {
        let mut visiting = HashSet::new();
        self.build_from_val(value, &mut visiting)
    }

    fn build_from_val(
        &mut self,
        value: Val,
        visiting: &mut HashSet<ObjKey>,
    ) -> MagResult<Rc<Package>> {
        let obj = value.as_obj().ok_or_else(|| {
            MagError::Generic("package definitions must be Jsonnet objects".into())
        })?;

        let key = ObjKey::new(obj.clone());

        if let Some(existing) = self.by_obj.get(&key) {
            return Ok(existing.clone());
        }

        if !visiting.insert(key.clone()) {
            return Err(MagError::DependencyCycle);
        }

        let result = (|| -> MagResult<Rc<Package>> {
            let name = read_package_name(&obj)?;
            let run_deps = self.collect_dependencies(&obj, "runDeps", visiting)?;
            let build_deps = self.collect_dependencies(&obj, "buildDeps", visiting)?;
            let build_script = read_build_script(&obj)?;
            let fetch = read_fetch_list(&obj)?;

            let hash = compute_hash(&build_script, &fetch, &run_deps, &build_deps);

            if let Some(existing) = self.by_hash.get(&hash) {
                self.by_obj.insert(key.clone(), existing.clone());
                return Ok(existing.clone());
            }

            let package = Rc::new(Package {
                name,
                build: build_script,
                hash: hash.clone(),
                run_deps,
                build_deps,
                fetch,
            });

            self.by_obj.insert(key.clone(), package.clone());
            self.by_hash.insert(hash, package.clone());

            Ok(package)
        })();
        visiting.remove(&key);
        result
    }

    fn collect_dependencies(
        &mut self,
        obj: &ObjValue,
        field: &str,
        visiting: &mut HashSet<ObjKey>,
    ) -> MagResult<Vec<Rc<Package>>> {
        let value = get_field(obj, field)?;

        let Some(value) = value else {
            return Ok(Vec::new());
        };

        match value {
            Val::Null => Ok(Vec::new()),
            Val::Arr(arr) => {
                let mut deps = Vec::with_capacity(arr.len());
                for (index, item) in arr.iter().enumerate() {
                    let val = item.map_err(|err| {
                        let message = format_jr_error(&err);
                        MagError::Evaluation {
                            context: format!(
                                "failed to evaluate dependency {index} in field '{field}'"
                            ),
                            message,
                            source: err,
                        }
                    })?;
                    let dep = self.build_from_val(val, visiting)?;
                    deps.push(dep);
                }
                Ok(deps)
            }
            other => Err(MagError::Generic(format!(
                "field '{field}' must be an array of packages, got {:?}",
                other.value_type()
            ))),
        }
    }
}

#[derive(Clone)]
struct ObjKey(ObjValue);

impl ObjKey {
    fn new(obj: ObjValue) -> Self {
        Self(obj)
    }
}

impl PartialEq for ObjKey {
    fn eq(&self, other: &Self) -> bool {
        ObjValue::ptr_eq(&self.0, &other.0)
    }
}

impl Eq for ObjKey {}

impl std::hash::Hash for ObjKey {
    fn hash<H: std::hash::Hasher>(&self, state: &mut H) {
        std::hash::Hash::hash(&self.0, state);
    }
}

fn get_field(obj: &ObjValue, field: &str) -> MagResult<Option<Val>> {
    obj.get(field.into()).map_err(|err| {
        let message = format_jr_error(&err);
        MagError::Evaluation {
            context: format!("failed to read field '{field}'"),
            message,
            source: err,
        }
    })
}

fn read_package_name(obj: &ObjValue) -> MagResult<Option<String>> {
    let value = get_field(obj, "name")?;

    match value {
        None | Some(Val::Null) => Ok(None),
        Some(Val::Str(s)) => {
            let name = s.to_string();
            validate_package_name(&name)?;
            Ok(Some(name))
        }
        Some(other) => Err(MagError::Generic(format!(
            "expected field 'name' to be a string, got {:?}",
            other.value_type()
        ))),
    }
}

fn validate_package_name(name: &str) -> MagResult<()> {
    if name.is_empty() {
        return Err(MagError::Generic(
            "package name must not be empty when provided".into(),
        ));
    }
    if name.contains('/') {
        return Err(MagError::Generic(
            "package name must not contain '/' characters".into(),
        ));
    }
    if name.contains('\n') || name.contains('\r') {
        return Err(MagError::Generic(
            "package name must not contain newline characters".into(),
        ));
    }
    Ok(())
}

fn read_build_script(obj: &ObjValue) -> MagResult<String> {
    let value = get_field(obj, "build")?;

    match value {
        None | Some(Val::Null) => Ok(String::new()),
        Some(Val::Str(s)) => Ok(s.to_string()),
        Some(other) => Err(MagError::Generic(format!(
            "expected field 'build' to be a string, got {:?}",
            other.value_type()
        ))),
    }
}

fn read_fetch_list(obj: &ObjValue) -> MagResult<Vec<FetchResource>> {
    let value = get_field(obj, "fetch")?;

    let Some(value) = value else {
        return Ok(Vec::new());
    };

    match value {
        Val::Null => Ok(Vec::new()),
        Val::Arr(arr) => {
            let mut out = Vec::with_capacity(arr.len());
            for (index, item) in arr.iter().enumerate() {
                let context = format!("fetch[{index}]");
                let val = item.map_err(|err| {
                    let message = format_jr_error(&err);
                    MagError::Evaluation {
                        context: format!("failed to evaluate {context}"),
                        message,
                        source: err,
                    }
                })?;
                let fetch_obj = val.as_obj().ok_or_else(|| {
                    MagError::Generic(format!(
                        "{context} must be an object, got {:?}",
                        val.value_type()
                    ))
                })?;

                let filename = read_required_string(&fetch_obj, "filename", &context)?;
                let sha256 = read_required_string(&fetch_obj, "sha256", &context)?;
                let urls = read_string_array(&fetch_obj, "urls", &context)?;

                out.push(FetchResource {
                    filename,
                    sha256,
                    urls,
                });
            }
            Ok(out)
        }
        other => Err(MagError::Generic(format!(
            "field 'fetch' must be an array of objects, got {:?}",
            other.value_type()
        ))),
    }
}

fn read_required_string(obj: &ObjValue, field: &str, context: &str) -> MagResult<String> {
    let value = get_field(obj, field)?;

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

fn read_string_array(obj: &ObjValue, field: &str, context: &str) -> MagResult<Vec<String>> {
    let value = get_field(obj, field)?;

    let Some(value) = value else {
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
                        context: format!("{context}: failed to evaluate urls[{index}]"),
                        message,
                        source: err,
                    }
                })?;
                match val {
                    Val::Str(s) => out.push(s.to_string()),
                    other => {
                        return Err(MagError::Generic(format!(
                            "{context}: expected urls[{index}] to be a string, got {:?}",
                            other.value_type()
                        )));
                    }
                }
            }
            Ok(out)
        }
        other => Err(MagError::Generic(format!(
            "{context}: expected field '{field}' to be an array of strings, got {:?}",
            other.value_type()
        ))),
    }
}

fn compute_hash(
    build: &str,
    fetch: &[FetchResource],
    run_deps: &[Rc<Package>],
    build_deps: &[Rc<Package>],
) -> String {
    let mut hasher = Sha256::new();
    hasher.update(b"build:");
    hasher.update(build.as_bytes());
    hasher.update(b"\0fetch\0");
    for item in fetch {
        hasher.update(item.filename.as_bytes());
        hasher.update(b"\0");
        hasher.update(item.sha256.as_bytes());
        hasher.update(b"\0");
    }
    hasher.update(b"\0run\0");
    for dep in run_deps {
        hasher.update(dep.hash.as_bytes());
    }
    hasher.update(b"\0build\0");
    for dep in build_deps {
        hasher.update(dep.hash.as_bytes());
    }
    let digest = hasher.finalize();
    format!("{:x}", digest)
}
