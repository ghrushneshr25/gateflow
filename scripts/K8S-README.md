# Gateflow local Kubernetes scripts

Lab / portfolio only. Not production.

## One command (everything)

```bash
cd /path/to/gateflow
./scripts/k8s-all-up.sh
```

Leaves port-forwards running. **Ctrl-C** stops forwards (cluster stays up).

```bash
./scripts/k8s-all-down.sh
# full wipe:
# GATEFLOW_K8S_DELETE_PVC=1 GATEFLOW_K8S_DELETE_CLUSTER=1 GATEFLOW_OBS_DELETE_NS=1 ./scripts/k8s-all-down.sh
```

## Separate scripts (in order)

| Script | Purpose |
|---|---|
| `k8s-tools-setup.sh` | kind + helm → `./.tools` |
| `k8s-cluster-up.sh` | create kind cluster `gateflow` |
| `k8s-build-images.sh` | docker build + `kind load` |
| `k8s-app-up.sh` | Helm app → ns **`gateflow`** |
| `k8s-obs-up.sh` | Helm obs → ns **`gateflow-obs`** |
| `k8s-smoke.sh` | Protocol A, VS1, fence, flash |
| `k8s-all-up.sh` | runs all of the above + PFs |
| `k8s-all-down.sh` | obs + app uninstall |
| `k8s-obs-down.sh` | obs only |
| `k8s-demo-down.sh` | app only |

## Makefile

```bash
make k8s-all-up
make k8s-all-down
make k8s-status
```

## Env knobs

| Variable | Default | Meaning |
|---|---|---|
| `GATEFLOW_SKIP_OBS` | 0 | skip obs namespace |
| `GATEFLOW_SKIP_SMOKE` | 0 | skip smoke tests |
| `GATEFLOW_SMOKE_FLASH` | 1 | flash in smoke |
| `GATEFLOW_KIND_CLUSTER` | gateflow | kind name |
| `GATEFLOW_IMAGE_TAG` | p8-local | image tag |
