# RBG Rollout Command

## Overview

The `kubectl rbg rollout` command manages the rollout lifecycle of RoleBasedGroup resources, including viewing revision history, comparing revisions, and rolling back to previous versions.

## Usage

### View Rollout History

```bash
# List all revisions
kubectl rbg rollout history <rbg-name>

# View details of a specific revision
kubectl rbg rollout history <rbg-name> --revision 1
```

### Compare Revisions

```bash
# Show diff between current RBG and a specific revision
kubectl rbg rollout diff <rbg-name> --revision 1
```

### Roll Back

```bash
# Rollback to the previous revision
kubectl rbg rollout undo <rbg-name>

# Rollback to a specific revision
kubectl rbg rollout undo <rbg-name> --revision 1
```

## Command Flags

### history `<rbg-name>`

| Flag | Default | Description |
|------|---------|-------------|
| `--revision` | `0` | View details of the specified revision (0 = list all) |

### diff `<rbg-name>`

| Flag | Default | Description |
|------|---------|-------------|
| `--revision` | - | Revision to compare against (required, must be > 0) |

### undo `<rbg-name>`

| Flag | Default | Description |
|------|---------|-------------|
| `--revision` | `0` | Rollback to the specified revision (0 = previous revision) |

## Example

```bash
# 1. List all revisions of an RBG
$ kubectl rbg rollout history nginx-cluster
Name                                 Revision
nginx-cluster-8676cf98bd-1           1
nginx-cluster-6f9cf75ddf-2           2

# 2. View details of revision 1
$ kubectl rbg rollout history nginx-cluster --revision=1

# 3. Compare current RBG with revision 1
$ kubectl rbg rollout diff nginx-cluster --revision=1
  (
        """
        ... // 34 identical lines
                - containerPort: 8080
                  protocol: TCP
-               resources: {}
+               resources:
+                 limits:
+                   memory: 512Mi
+                 requests:
+                   memory: 100Mi
          workload:
            apiVersion: apps/v1
        ... // 2 identical lines
        """
  )

# 4. Rollback to revision 1
$ kubectl rbg rollout undo nginx-cluster --revision=1
rbg nginx-cluster rollback to revision 1 successfully
```
