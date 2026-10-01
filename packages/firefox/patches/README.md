Except for `nspr-poll.patch`, these portability patches come from Alpine Linux's `community/firefox-esr`
package at aports commit `0d13d1ca28b25a3841f62bd3adb2d59d9d7b0d36`:

https://github.com/alpinelinux/aports/tree/0d13d1ca28b25a3841f62bd3adb2d59d9d7b0d36/community/firefox-esr

Original authorship and upstream references are retained in the patches.
Patch contexts and hunk offsets are refreshed for Firefox 140.17.0esr.
They address musl's lack of glibc interfaces and let the build select the
native Rust target explicitly. The recipe embeds their contents with Jsonnet
`importstr`, so changes to a patch change the package identity.

`nspr-poll.patch` is a local build correction: musl has `poll()`, so bundled
NSPR must not replace it with its legacy `select()` emulation. This prevents
a sandbox violation loop in the media process without changing Firefox's
sandbox policy. Chromium's standalone NSPR recipe already defines the same
`_PR_POLL_AVAILABLE` setting. Real browser playback is covered by the VM audio
test in `magnet-linux/tests/session-boot.sh`.
