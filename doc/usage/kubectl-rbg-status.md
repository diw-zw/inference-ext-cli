# RBG Status Command

## Overview

The `kubectl rbg status` command displays comprehensive status information for a RoleBasedGroup resource, including role readiness, replica counts, and progress visualization.

## Usage

```bash
kubectl rbg status <rbg-name>
```

## Output

The command displays:

- **Resource Overview** — namespace, name, and age
- **Role Statuses** — each role with ready/total replica counts and a progress bar
- **Summary** — total roles and overall ready replica count

### Example

```bash
$ kubectl rbg status nginx-cluster -n default
📊 Resource Overview
  Namespace: default
  Name:      nginx-cluster

  Age:       50s

📦 Role Statuses
leader       1/1                (total: 1)      [████████████████] 100%
worker       3/3                (total: 3)      [████████████████] 100%

∑ Summary: 2 roles | 4/4 Ready
```
