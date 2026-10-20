# Isolated deployment and rollback

App: `beszel-intel-lab`, installed through TrueNAS middleware Custom App YAML
configuration (`app.create custom_app=true`). Mac hub: local Docker port 18090,
reachable only through the existing LAN configuration. Dedicated generated lab
user, key, token, fingerprint and named-volume storage; no production identity.
No Cloudflare/public endpoints or registry publication were configured.

Agent volume is TrueNAS-managed Docker storage under
`/mnt/.ix-apps/docker/volumes/ix-beszel-intel-lab_agent-data/_data`.
`docker.config` identifies nvme_pool/ix-apps. No persistent files on boot pool.
/sys is read-only; no Docker socket, render node or host /proc mounts. No host
PID or network namespace, published agent ports, privileges or added caps.
All capabilities dropped. Root filesystem read-only, /tmp size-limited tmpfs.
SENSORS empty disables generic temperature collection for the comparison.
Generic fan collection retains upstream behavior; no Intel fan inputs existed.

Build from repo root after the documented cross-build:

```sh
docs/gpu-intel-truenas/build-image.sh
# Save the printed commit-tagged image, transfer locally, load on TrueNAS.
docker save beszel-intel-lab:<commit-sha> | gzip -1 > image.tar.gz
```

Dockerfile pins official Intel 0.21.0 by digest; only /agent changes. Minimal
context avoids upstream .dockerignore excluding build/ and copying dependencies.
Base architecture/entrypoint/utilities were inspected. No public image push.
Local image load used Portainer endpoint's Docker images/load API. Deployment
uses TrueNAS app management rather than a new Portainer stack.

Render compose.yaml with dedicated lab placeholders. Keep rendered credentials
outside Git and reports. Never substitute production credentials. `pull_policy:
never` ensures the explicit locally loaded development image is used.

Rollback stops only this custom app:

```sh
midclt call -j app.stop beszel-intel-lab
# Restart only if further lab testing is intended:
midclt call -j app.start beszel-intel-lab
```

Optional eventual removal (preserve managed volumes and local images):

```sh
midclt call -j app.delete beszel-intel-lab \
  '{"remove_images":false,"remove_ix_volumes":false}'
```

Do not remove shared images or data, start the existing production container,
or modify production stacks as part of lab rollback. Stop/start verification
results and final lab state are recorded in START_HERE.md.

For a fresh repeat, [hub.compose.yaml](hub.compose.yaml) describes the separate
Mac hub. Set its verified LAN address, create a dedicated lab account and system,
and use that system's generated key/token in the agent template. Confirm the
TrueNAS host can reach the LAN hub before starting the agent. Existing lab
credentials and data are retained privately for this run; no credentials need
to be regenerated merely to restart it. The actual run used the same image,
port and named-volume arrangement through Docker CLI.

Final rollback was tested again after experimental checks: restored the core
image/configuration, verified ten realtime updates with power, then stopped only
`beszel-intel-lab`. Final state STOPPED; managed volumes/images were retained.
The Mac hub remains available; no production service was restarted or altered.
