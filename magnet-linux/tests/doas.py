#!/usr/bin/env python3
"""Check doas in a booted test VM using its pinned SSH key and private password."""
import os
from pathlib import Path
import select
import shlex
import subprocess
import sys
import time

vm = Path(sys.argv[1]).resolve()
port = sys.argv[2]
credentials = dict(line.split(': ', 1) for line in
                   (vm / 'credentials.txt').read_text().splitlines() if ': ' in line)
user = (vm / 'login-user').read_text().strip()
password = credentials['Password']
ssh = ['ssh', '-F', '/dev/null', '-o', 'BatchMode=yes', '-o', 'IdentitiesOnly=yes',
       '-o', 'StrictHostKeyChecking=yes', '-o', 'HostKeyAlias=magnet-desktop-vm',
       '-o', f'UserKnownHostsFile={vm / "known_hosts"}',
       '-o', 'GlobalKnownHostsFile=/dev/null', '-o', 'ConnectTimeout=3',
       '-i', str(vm / 'client-key'), '-p', port]


def run(command, expected=0, authenticate=None):
    process = subprocess.Popen(ssh + ['-tt' if authenticate is not None else '-T',
                                     f'{user}@127.0.0.1', command],
                               stdin=subprocess.PIPE, stdout=subprocess.PIPE,
                               stderr=subprocess.STDOUT)
    output = b''
    try:
        if authenticate is not None:
            deadline = time.monotonic() + 20
            while b'password:' not in output.lower():
                remaining = deadline - time.monotonic()
                if remaining <= 0 or not select.select([process.stdout], [], [], remaining)[0]:
                    raise RuntimeError('Timed out waiting for the doas password prompt')
                chunk = os.read(process.stdout.fileno(), 4096)
                if not chunk:
                    raise RuntimeError('doas exited before asking for a password')
                output += chunk
            process.stdin.write((authenticate + '\n').encode())
            process.stdin.flush()
        output += process.communicate(timeout=30)[0]
        result = output.decode(errors='replace')
        for secret in (password, credentials.get('Root password', ''), authenticate):
            if secret:
                result = result.replace(secret, '[redacted]')
        if process.returncode != expected:
            raise AssertionError(f'Expected status {expected}, got {process.returncode}: {result}')
        return result
    finally:
        if process.poll() is None:
            process.kill()
            process.wait()


run('set -eu; test "$(stat -c %u:%g:%a /bin/doas)" = 0:0:4755; '
    'id -nG | tr " " "\n" | grep -qx wheel; '
    'for account in nobody greeter; do '
    'if id -nG "$account" | tr " " "\n" | grep -qx wheel; then exit 1; fi; done')
assert 'Authentication required' in run('doas -n /bin/true', expected=1)
assert 'Authentication failed' in run('doas /bin/true', expected=1,
                                     authenticate='magnet-intentionally-wrong-password')
check = ('test "$(id -u)" = 0; test "$HOME" = /root; test "$USER" = root; '
         'test "$LOGNAME" = root; test "$DOAS_USER" = ' + shlex.quote(user) + '; '
         'test -z "${MAGNET_DOAS_TEST:-}"; test "${PATH%%:*}" = /bin; '
         'test "$(stat -c %u:%g:%a /etc/doas.conf)" = 0:0:600; '
         'test "$(stat -c %u:%g:%a /etc/pam.d/doas)" = 0:0:644; '
         '/bin/doas -C /etc/doas.conf; printf "MAGNET_DOAS_OK\\n"')
result = run('MAGNET_DOAS_TEST=discarded doas /bin/sh -ec ' + shlex.quote(check),
             authenticate=password)
assert 'MAGNET_DOAS_OK' in result
run('doas /bin/sh -c "exit 42"', expected=42, authenticate=password)
# A successful invocation must not make a later command passwordless, even in
# the same shell/terminal. The ordinary user's password is used throughout.
result = run('doas /bin/true && if doas -n /bin/true; then exit 99; fi',
             authenticate=password)
assert 'Authentication required' in result
for account in ('nobody', 'greeter'):
    result = run(f'doas /bin/su -s /bin/sh {account} -c ' +
                 shlex.quote('/bin/doas -n /bin/id -u'), expected=1,
                 authenticate=password)
    assert 'Operation not permitted' in result
assert 'Operation not permitted' in run('doas -n -u nobody /bin/id -u', expected=1)
print('PASS: doas own-password authentication, rejected wrong password, root identity, '
      'clean environment, file permissions, exit status, no password caching, '
      'and denied non-admin accounts/other target users')
