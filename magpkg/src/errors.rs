use jrsonnet_evaluator::{
    error::Error as JrError,
    trace::{CompactFormat, PathResolver, TraceFormat},
};

pub fn format_jr_error(err: &JrError) -> String {
    let format = CompactFormat {
        resolver: PathResolver::new_cwd_fallback(),
        ..Default::default()
    };

    format.format(err).unwrap_or_else(|_| err.to_string())
}
