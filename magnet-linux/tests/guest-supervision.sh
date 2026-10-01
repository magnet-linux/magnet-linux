#!/bin/sh
# Only run inside the disposable installation-test guest.
set -eu
test "$(readlink /proc/1/exe)" = /bin/sinit
export PERP_BASE=/etc/perp
service=magnet-test-supervision
test ! -e "$PERP_BASE/$service"
mkdir "$PERP_BASE/$service"
cat > "$PERP_BASE/$service/rc.main" <<'EOF'
#!/bin/sh
test "$1" = start || exit 0
echo "$$" > /run/magnet-supervision.pid
exec sleep 600
EOF
chmod 0755 "$PERP_BASE/$service/rc.main"
perpctl A "$service"
for _ in $(seq 1 20); do
  test ! -f /run/magnet-supervision.pid || break
  sleep 1
done
first=$(cat /run/magnet-supervision.pid)
kill -KILL "$first"
for _ in $(seq 1 20); do
  test "$(cat /run/magnet-supervision.pid)" = "$first" || break
  sleep 1
done
second=$(cat /run/magnet-supervision.pid)
test "$second" != "$first"
perpok -u 1 "$service" || { sleep 2; perpok -u 1 "$service"; }
perpctl d "$service"
sleep 3
if kill -0 "$second" 2>/dev/null; then exit 1; fi
test "$(cat /run/magnet-supervision.pid)" = "$second"
perpctl u "$service"
for _ in $(seq 1 20); do
  test "$(cat /run/magnet-supervision.pid)" = "$second" || break
  sleep 1
done
test "$(cat /run/magnet-supervision.pid)" != "$second"
perpctl X "$service"
sleep 2
if perpok "$service"; then exit 1; fi
rm -rf "${PERP_BASE:?}/${service:?}"
rm /run/magnet-supervision.pid
echo MAGNET_TEST_SUPERVISION_OK
