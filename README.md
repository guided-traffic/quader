# Quader Helm repository

Helm charts of [Quader](https://github.com/guided-traffic/quader), published by the release workflow.

```bash
helm repo add quader https://guided-traffic.github.io/quader/
helm search repo quader
```

| Chart | What it installs |
|---|---|
| `quader` | Quader itself |
| `quader-bastion` | the Quader Bastion, the internet-facing read-only cache of a main installation |

Releases before 0.56.0 were published under the project's former name and are not offered here.
