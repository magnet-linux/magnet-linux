local system = (import './system.jsonnet').system;
{
  live: system {
    name: 'magnet-linux-installer',
  },
}
