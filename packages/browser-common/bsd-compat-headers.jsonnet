// BSD compatibility headers maintained by Alpine; each header contains its license.
{
  headers: {
    name: 'bsd-compat-headers-0.7.2',
    buildEnv: { PATH: '/bin' },
    buildDeps: [(import '../bootstrap.jsonnet').bootstrap],
    runDeps: [],
    build: { kind: 'script', script: |||
      mkdir -p /out/include/sys /out/share/licenses/bsd-compat-headers
      cp /fetch/*.h /out/include/sys/
      cp /fetch/*.h /out/share/licenses/bsd-compat-headers/
    ||| },
    fetch: [
    {
        "filename": "cdefs.h",
        "sha256": "15b1dd86710a4c5fc0654dbd871965b227474eb7f54531eebafffe8c1061f10e",
        "urls": [
            "https://raw.githubusercontent.com/alpinelinux/aports/a44854115ef1a1cda0c41b3e6f0974f51755d736/main/bsd-compat-headers/cdefs.h"
        ]
    },
    {
        "filename": "queue.h",
        "sha256": "c13407edd0e33be73cae72514cb234f8612e1c0e54401c9448daffd3a240158b",
        "urls": [
            "https://raw.githubusercontent.com/alpinelinux/aports/a44854115ef1a1cda0c41b3e6f0974f51755d736/main/bsd-compat-headers/queue.h"
        ]
    },
    {
        "filename": "tree.h",
        "sha256": "e0ea17940260f9c1fdd2d35f74eec141d21f09844eaea91131993cb950901a16",
        "urls": [
            "https://raw.githubusercontent.com/alpinelinux/aports/a44854115ef1a1cda0c41b3e6f0974f51755d736/main/bsd-compat-headers/tree.h"
        ]
    }
],
  },
}
