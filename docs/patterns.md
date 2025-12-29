# ACK Controller Update Patterns

This document catalogs the most common update patterns observed across 60+ AWS Controllers for Kubernetes (ACK) service controllers. These patterns inform how the code generator should handle complex update scenarios.

## Table of Contents

1. [Generalized Pattern Categories](#generalized-pattern-categories)
2. [Pattern Hierarchy](#pattern-hierarchy)
3. [Pattern Summary](#pattern-summary)
4. [Universal Patterns](#universal-patterns)
5. [State Management Patterns](#state-management-patterns)
6. [Field Update Patterns](#field-update-patterns)
7. [Sub-Resource Patterns](#sub-resource-patterns)
8. [Comparison Patterns](#comparison-patterns)
9. [AWS API Constraint Patterns](#aws-api-constraint-patterns)
10. [Pattern Frequency](#pattern-frequency)
11. [Controller-Specific Examples](#controller-specific-examples)

---

## Generalized Pattern Categories

The specific patterns observed in ACK controllers can be abstracted into higher-level categories. Understanding these abstractions enables more flexible code generation.

### Collection Sync Patterns

These patterns handle collections of items that need synchronization between desired and actual state.

#### 1. Map Sync Pattern

**What it is:** Synchronization of key-value maps where keys are identifiers and values are the data.

**Characteristics:**
- Compute delta: added keys, updated keys (same key, different value), removed keys
- Call Remove/Untag API for removed keys
- Call Add/Put/Tag API for added/updated keys
- Order-independent comparison

**Instances:**
| Field Type | Example | Add API | Remove API |
|------------|---------|---------|------------|
| Tags | `map[string]*string` | `TagResource` | `UntagResource` |
| Attributes | SNS/SQS attributes | `SetAttributes` | `SetAttributes` (empty) |
| Labels | EKS Nodegroup labels | `UpdateNodegroupConfig` | `UpdateNodegroupConfig` |
| Environment | Lambda env vars | `UpdateFunctionConfiguration` | `UpdateFunctionConfiguration` |

**Generic Implementation:**
```go
type MapSyncConfig struct {
    FieldPath    string                                    // e.g., "Spec.Tags"
    KeyField     string                                    // e.g., "Key"
    ValueField   string                                    // e.g., "Value"
    AddFunc      func(ctx, key, value) error              // API to add/update
    RemoveFunc   func(ctx, key) error                     // API to remove
    BatchSize    int                                       // 0 = no batching
}

func (rm *resourceManager) syncMap(ctx, desired, latest, config MapSyncConfig) error {
    added, updated, removed := computeMapDelta(desired, latest, config)

    // Remove first
    for _, key := range removed {
        if err := config.RemoveFunc(ctx, key); err != nil {
            return err
        }
    }

    // Add/Update
    for _, item := range append(added, updated...) {
        if err := config.AddFunc(ctx, item.Key, item.Value); err != nil {
            return err
        }
    }

    return nil
}
```

#### 2. Set Sync Pattern

**What it is:** Synchronization of unordered collections where items are identified by their value (no separate key).

**Characteristics:**
- Compute delta: items to add, items to remove
- No "update" concept - items are either present or absent
- Order-independent comparison
- Remove before add (avoid conflicts)

**Instances:**
| Field Type | Example | Add API | Remove API |
|------------|---------|---------|------------|
| Policies | IAM managed policies | `AttachRolePolicy` | `DetachRolePolicy` |
| Security Groups | VPC endpoint SGs | `ModifyVpcEndpoint` | `ModifyVpcEndpoint` |
| Subnets | VPC endpoint subnets | `ModifyVpcEndpoint` | `ModifyVpcEndpoint` |
| Client IDs | OIDC provider | `AddClientIDToOpenIDConnectProvider` | `RemoveClientIDFromOpenIDConnectProvider` |
| Access Policies | EKS access entry | `AssociateAccessPolicy` | `DisassociateAccessPolicy` |
| Ingress Rules | Security group | `AuthorizeSecurityGroupIngress` | `RevokeSecurityGroupIngress` |

**Generic Implementation:**
```go
type SetSyncConfig struct {
    FieldPath    string                           // e.g., "Spec.Policies"
    EqualFunc    func(a, b interface{}) bool     // Custom equality
    AddFunc      func(ctx, item) error           // API to add
    RemoveFunc   func(ctx, item) error           // API to remove
    BatchSize    int                              // 0 = no batching
}

func (rm *resourceManager) syncSet(ctx, desired, latest, config SetSyncConfig) error {
    toAdd, toRemove := computeSetDelta(desired, latest, config.EqualFunc)

    // Remove first (avoid conflicts)
    for _, item := range toRemove {
        if err := config.RemoveFunc(ctx, item); err != nil {
            return err
        }
    }

    // Add
    for _, item := range toAdd {
        if err := config.AddFunc(ctx, item); err != nil {
            return err
        }
    }

    return nil
}
```

#### 3. List Sync Pattern (with Identity)

**What it is:** Synchronization of collections where items have a unique identity (key/index) and can be individually created, updated, or deleted.

**Characteristics:**
- Compute delta: items to add, items to update (same key, different values), items to delete
- Items identified by key field(s) - e.g., rule number, route destination
- Separate APIs for create vs update vs delete
- Order of operations: delete → update → add

**Instances:**
| Field Type | Key Field(s) | Create API | Update API | Delete API |
|------------|--------------|------------|------------|------------|
| Network ACL entries | `RuleNumber + Egress` | `CreateNetworkAclEntry` | `ReplaceNetworkAclEntry` | `DeleteNetworkAclEntry` |
| Routes | `DestinationCidrBlock` | `CreateRoute` | `ReplaceRoute` | `DeleteRoute` |
| Prefix list entries | `Cidr` | `ModifyManagedPrefixList` | `ModifyManagedPrefixList` | `ModifyManagedPrefixList` |
| GSIs (DynamoDB) | `IndexName` | `UpdateTable` | `UpdateTable` | `UpdateTable` |

**Generic Implementation:**
```go
type ListSyncConfig struct {
    FieldPath     string                                      // e.g., "Spec.Entries"
    KeyFunc       func(item interface{}) string              // Extract key
    EqualFunc     func(a, b interface{}) bool                // Full equality
    CreateFunc    func(ctx, item) error                      // API to create
    UpdateFunc    func(ctx, item) error                      // API to update/replace
    DeleteFunc    func(ctx, item) error                      // API to delete
    FilterFunc    func(items) items                          // Filter AWS-managed items
}

func (rm *resourceManager) syncList(ctx, desired, latest, config ListSyncConfig) error {
    // Filter AWS-managed items
    desiredFiltered := config.FilterFunc(desired)
    latestFiltered := config.FilterFunc(latest)

    toAdd, toUpdate, toDelete := computeListDelta(desiredFiltered, latestFiltered, config)

    // Delete first
    for _, item := range toDelete {
        if err := config.DeleteFunc(ctx, item); err != nil {
            return err
        }
    }

    // Update (same key, different values)
    for _, item := range toUpdate {
        if err := config.UpdateFunc(ctx, item); err != nil {
            return err
        }
    }

    // Add
    for _, item := range toAdd {
        if err := config.CreateFunc(ctx, item); err != nil {
            return err
        }
    }

    return nil
}
```

### Scalar Field Patterns

These patterns handle individual fields that map to specific API operations.

#### 4. Put-or-Delete Pattern

**What it is:** A field where a value means "put/set" and nil/empty means "delete".

**Characteristics:**
- Check if field is nil or empty
- If empty: call Delete API
- If not empty: call Put/Set API
- Often used for optional configurations

**Instances:**
| Field | Put API | Delete API |
|-------|---------|------------|
| LifecyclePolicy (ECR) | `PutLifecyclePolicy` | `DeleteLifecyclePolicy` |
| RepositoryPolicy (ECR) | `SetRepositoryPolicy` | `DeleteRepositoryPolicy` |
| FunctionConcurrency (Lambda) | `PutFunctionConcurrency` | `DeleteFunctionConcurrency` |
| ResourcePolicy (various) | `PutResourcePolicy` | `DeleteResourcePolicy` |

**Generic Implementation:**
```go
type PutOrDeleteConfig struct {
    FieldPath   string                          // e.g., "Spec.LifecyclePolicy"
    IsEmpty     func(value interface{}) bool   // Check if empty/nil
    PutFunc     func(ctx, value) error         // API to put/set
    DeleteFunc  func(ctx) error                // API to delete
}

func (rm *resourceManager) syncPutOrDelete(ctx, desired, config PutOrDeleteConfig) error {
    value := getFieldValue(desired, config.FieldPath)

    if config.IsEmpty(value) {
        return config.DeleteFunc(ctx)
    }
    return config.PutFunc(ctx, value)
}
```

#### 5. Simple Put Pattern

**What it is:** A field that is updated via a Put/Set/Modify API.

**Characteristics:**
- Direct field-to-API mapping
- No delete concept (field always has a value)
- May support nil as "use default"

**Instances:**
| Field | API |
|-------|-----|
| ImageTagMutability (ECR) | `PutImageTagMutability` |
| ImageScanningConfiguration (ECR) | `PutImageScanningConfiguration` |
| EnableDNSSupport (VPC) | `ModifyVpcAttribute` |
| EnableDNSHostnames (VPC) | `ModifyVpcAttribute` |

#### 6. Association Pattern

**What it is:** A relationship where attach/associate returns an ID needed for detach/disassociate.

**Characteristics:**
- Associate/Attach returns an association ID
- Must store association ID in status
- Disassociate/Detach requires the association ID
- Often 1:1 or 1:N relationships

**Instances:**
| Resource | Associate API | Disassociate API | ID Field |
|----------|---------------|------------------|----------|
| VPC CIDR Block | `AssociateVpcCidrBlock` | `DisassociateVpcCidrBlock` | `AssociationId` |
| DHCP Options | `AssociateDhcpOptions` | N/A (replace) | N/A |
| Internet Gateway | `AttachInternetGateway` | `DetachInternetGateway` | N/A |
| Network ACL Association | `ReplaceNetworkAclAssociation` | N/A | `AssociationId` |
| Route Table Association | `AssociateRouteTable` | `DisassociateRouteTable` | `AssociationId` |

**Generic Implementation:**
```go
type AssociationConfig struct {
    FieldPath       string                                    // e.g., "Spec.CIDRBlocks"
    StatusPath      string                                    // e.g., "Status.CIDRBlockAssociationSet"
    AssociateFunc   func(ctx, item) (associationID, error)   // Returns ID
    DisassociateFunc func(ctx, associationID) error          // Uses ID
    GetAssociationID func(status, item) string               // Lookup ID from status
}

func (rm *resourceManager) syncAssociations(ctx, desired, latest, config AssociationConfig) error {
    toAdd, toRemove := computeSetDelta(desired, latest)

    // Disassociate by ID
    for _, item := range toRemove {
        assocID := config.GetAssociationID(latest.Status, item)
        if err := config.DisassociateFunc(ctx, assocID); err != nil {
            return err
        }
    }

    // Associate and capture IDs
    for _, item := range toAdd {
        assocID, err := config.AssociateFunc(ctx, item)
        if err != nil {
            return err
        }
        // Store association ID in status for future disassociation
        updateStatusWithAssociationID(latest.Status, item, assocID)
    }

    return nil
}
```

### Lifecycle Patterns

These patterns handle resource lifecycle and state management.

#### 7. State Gate Pattern

**What it is:** Block or allow updates based on resource state.

**Characteristics:**
- Check current state before modifications
- Block if transitional: CREATING, UPDATING, DELETING
- Terminal if failed: FAILED, DELETE_FAILED
- Proceed only if stable: ACTIVE, AVAILABLE, RUNNING

**Generic Implementation:**
```go
type StateGateConfig struct {
    StatusField      string                    // e.g., "Status.Status"
    ActiveStates     []string                  // States allowing updates
    TransitionalStates []string                // States requiring requeue
    TerminalStates   []string                  // States stopping reconciliation
    RequeueDelay     time.Duration             // Delay for transitional states
}

func (rm *resourceManager) checkStateGate(latest, config StateGateConfig) (proceed bool, result error) {
    state := getFieldValue(latest, config.StatusField)

    if inSlice(state, config.TerminalStates) {
        msg := fmt.Sprintf("Resource is in terminal state: %s", state)
        ackcondition.SetTerminal(latest, corev1.ConditionTrue, &msg, nil)
        return false, nil
    }

    if inSlice(state, config.TransitionalStates) {
        msg := fmt.Sprintf("Resource is in %s state", state)
        ackcondition.SetSynced(latest, corev1.ConditionFalse, &msg, nil)
        return false, ackrequeue.NeededAfter(errors.New(msg), config.RequeueDelay)
    }

    if !inSlice(state, config.ActiveStates) {
        return false, ackrequeue.NeededAfter(
            fmt.Errorf("waiting for resource to be active, current: %s", state),
            config.RequeueDelay,
        )
    }

    return true, nil
}
```

#### 8. Version Pattern

**What it is:** Resources where updates create new versions instead of in-place modifications.

**Characteristics:**
- Cannot modify existing versions
- Create new version for updates
- May have version limits (e.g., IAM Policy has 5)
- May need to delete old versions
- May set new version as default

**Instances:**
| Resource | Create Version API | Version Limit | Delete API |
|----------|-------------------|---------------|------------|
| IAM Policy | `CreatePolicyVersion` | 5 | `DeletePolicyVersion` |
| Launch Template | `CreateLaunchTemplateVersion` | Unlimited | `DeleteLaunchTemplateVersion` |
| Lambda Function | `PublishVersion` | Unlimited | N/A |

**Generic Implementation:**
```go
type VersionConfig struct {
    MaxVersions       int                                     // 0 = unlimited
    CreateVersionFunc func(ctx, desired) (versionID, error)  // Create new version
    ListVersionsFunc  func(ctx) ([]Version, error)           // List existing versions
    DeleteVersionFunc func(ctx, versionID) error             // Delete old version
    SetDefaultFunc    func(ctx, versionID) error             // Set as default (optional)
}

func (rm *resourceManager) updateWithVersion(ctx, desired, config VersionConfig) error {
    // Check version limit
    if config.MaxVersions > 0 {
        versions, _ := config.ListVersionsFunc(ctx)
        if len(versions) >= config.MaxVersions {
            // Delete oldest non-default version
            for _, v := range versions {
                if !v.IsDefault {
                    config.DeleteVersionFunc(ctx, v.ID)
                    break
                }
            }
        }
    }

    // Create new version
    versionID, err := config.CreateVersionFunc(ctx, desired)
    if err != nil {
        return err
    }

    // Set as default if needed
    if config.SetDefaultFunc != nil {
        return config.SetDefaultFunc(ctx, versionID)
    }

    return nil
}
```

### Constraint Patterns

These patterns handle AWS API limitations.

#### 9. Batch Pattern

**What it is:** Split large operations into batches to respect AWS API limits.

**Characteristics:**
- AWS APIs have per-request limits
- Must chunk requests
- All chunks must succeed (or handle partial failure)

**Known Limits:**
| Service | API | Limit |
|---------|-----|-------|
| EC2 | Security Group Rules | 1000 per call |
| ElastiCache | Parameters | 20 per call |
| RDS | Parameters | 20 per call |

**Generic Implementation:**
```go
type BatchConfig struct {
    BatchSize   int
    ProcessFunc func(ctx, batch []Item) error
}

func (rm *resourceManager) processBatched(ctx, items []Item, config BatchConfig) error {
    for i := 0; i < len(items); i += config.BatchSize {
        end := min(i + config.BatchSize, len(items))
        batch := items[i:end]

        if err := config.ProcessFunc(ctx, batch); err != nil {
            return err
        }
    }
    return nil
}
```

#### 10. Sequential Field Pattern

**What it is:** Update fields one at a time due to API constraints.

**Characteristics:**
- API only accepts one field per call
- Must requeue after each field update
- Continue until all fields updated

**Instances:**
- EC2 `ModifyInstanceAttribute` (one attribute per call)

**Generic Implementation:**
```go
type SequentialFieldConfig struct {
    Fields []FieldConfig  // Ordered list of fields to check
}

type FieldConfig struct {
    Path       string
    UpdateFunc func(ctx, desired) error
}

func (rm *resourceManager) updateSequential(ctx, delta, desired, config SequentialFieldConfig) error {
    for _, field := range config.Fields {
        if delta.DifferentAt(field.Path) {
            if err := field.UpdateFunc(ctx, desired); err != nil {
                return err
            }
            // Requeue to process next field
            return ackrequeue.Needed(errors.New("processing next field"))
        }
    }
    return nil // All fields processed
}
```

---

## Pattern Hierarchy

```
                              ┌─────────────────────────┐
                              │    Update Operation     │
                              └───────────┬─────────────┘
                                          │
          ┌───────────────────────────────┼───────────────────────────────┐
          │                               │                               │
          ▼                               ▼                               ▼
┌─────────────────────┐       ┌─────────────────────┐       ┌─────────────────────┐
│ Collection Patterns │       │   Scalar Patterns   │       │ Lifecycle Patterns  │
└─────────┬───────────┘       └─────────┬───────────┘       └─────────┬───────────┘
          │                             │                             │
    ┌─────┼─────┐               ┌───────┼───────┐             ┌───────┼───────┐
    │     │     │               │       │       │             │       │       │
    ▼     ▼     ▼               ▼       ▼       ▼             ▼       ▼       ▼
┌─────┐┌─────┐┌─────┐     ┌─────┐ ┌─────┐ ┌─────┐       ┌─────┐ ┌─────┐ ┌─────┐
│ Map ││ Set ││List │     │Put/ │ │Simp-│ │Asso-│       │State│ │Vers-│ │Async│
│Sync ││Sync ││Sync │     │Del  │ │le   │ │ciat-│       │Gate │ │ion  │ │     │
└─────┘└─────┘└─────┘     └─────┘ └─────┘ └─────┘       └─────┘ └─────┘ └─────┘
   │                                 │                              │
   ▼                                 ▼                              ▼
┌─────┐                        ┌───────────┐                  ┌───────────┐
│Tags │ (instance)             │Constraint │                  │ Condition │
└─────┘                        │ Patterns  │                  │Management │
                               └─────┬─────┘                  └───────────┘
                                     │
                               ┌─────┼─────┐
                               │           │
                               ▼           ▼
                          ┌───────┐   ┌────────┐
                          │ Batch │   │Sequen- │
                          │       │   │tial    │
                          └───────┘   └────────┘
```

### Pattern Composition

Real-world updates often combine multiple patterns:

```go
func (rm *resourceManager) customUpdate(ctx, desired, latest, delta) (*resource, error) {
    // 1. State Gate Pattern
    if proceed, err := rm.checkStateGate(latest); !proceed {
        return latest, err
    }

    // 2. Map Sync Pattern (Tags)
    if delta.DifferentAt("Spec.Tags") {
        if err := rm.syncMap(ctx, desired, latest, tagSyncConfig); err != nil {
            return nil, err
        }
    }

    // 3. Set Sync Pattern (Policies)
    if delta.DifferentAt("Spec.Policies") {
        if err := rm.syncSet(ctx, desired, latest, policySyncConfig); err != nil {
            return nil, err
        }
    }

    // 4. List Sync Pattern (Entries)
    if delta.DifferentAt("Spec.Entries") {
        if err := rm.syncList(ctx, desired, latest, entrySyncConfig); err != nil {
            return nil, err
        }
    }

    // 5. Put-or-Delete Pattern (LifecyclePolicy)
    if delta.DifferentAt("Spec.LifecyclePolicy") {
        if err := rm.syncPutOrDelete(ctx, desired, lifecyclePolicyConfig); err != nil {
            return nil, err
        }
    }

    return desired, nil
}
```

---

## Pattern Summary

| Pattern | Frequency | Key Characteristic |
|---------|-----------|-------------------|
| Tag Synchronization | 95%+ | Universal, always first |
| State-Based Gating | 70%+ | Block updates during async ops |
| Delta-Based Field Routing | 65%+ | Route to specific sync methods |
| Set-Based Add/Remove | 50%+ | Compute delta, delete then add |
| Independent Field Updates | 45%+ | Each field has own API |
| PreCompare Normalization | 40%+ | Normalize before comparison |
| Delete-on-Empty | 35%+ | Null/empty means delete |
| Async Update + Synced=False | 30%+ | Mark condition, requeue |
| Entry-Level CUD | 25%+ | Sub-resources managed individually |
| Association ID Tracking | 20%+ | Track IDs for disassociation |
| Batch Processing | 15%+ | AWS API batch limits |
| One-Attribute-Per-Call | 10%+ | AWS API limitation |
| Version Management | 5%+ | Create versions, manage limits |

---

## Universal Patterns

### 1. Tag Synchronization

**Frequency:** 95%+ of controllers

Tags are the most universally implemented update pattern. They are always handled independently and typically processed first.

```go
if delta.DifferentAt("Spec.Tags") {
    err := tags.Sync(
        ctx,
        rm.sdkapi,
        rm.metrics,
        string(*latest.ko.Status.ACKResourceMetadata.ARN),
        desired.ko.Spec.Tags,
        latest.ko.Spec.Tags,
    )
    if err != nil {
        return nil, err
    }
}
```

**Implementation Details:**
- Uses centralized `tags.Sync()` helper from ACK runtime
- Computes three sets: `toAdd`, `toUpdate`, `toRemove`
- Calls `TagResource` and `UntagResource` APIs
- Always independent of other field updates

**Tag Delta Computation:**
```go
func computeTagsDelta(desired, latest []*svcapitypes.Tag) (added, updated, removed []*svcapitypes.Tag) {
    latestMap := make(map[string]*string)
    for _, tag := range latest {
        latestMap[*tag.Key] = tag.Value
    }

    for _, tag := range desired {
        if latestVal, exists := latestMap[*tag.Key]; exists {
            if !equalStrings(tag.Value, latestVal) {
                updated = append(updated, tag)
            }
            delete(latestMap, *tag.Key)
        } else {
            added = append(added, tag)
        }
    }

    for key := range latestMap {
        removed = append(removed, &svcapitypes.Tag{Key: &key})
    }
    return
}
```

### 2. Standard customUpdate Structure

Most controllers follow this structure:

```go
func (rm *resourceManager) customUpdate<Resource>(
    ctx context.Context,
    desired *resource,
    latest *resource,
    delta *ackcompare.Delta,
) (updated *resource, err error) {
    rlog := ackrtlog.FromContext(ctx)
    exit := rlog.Trace("rm.customUpdate<Resource>")
    defer func() { exit(err) }()

    // 1. Initialize updated from desired
    updated = rm.concreteResource(desired.DeepCopy())
    updated.SetStatus(latest)

    // 2. State gating (if applicable)
    if isDeleting(latest) {
        return updated, requeueWaitWhileDeleting
    }

    // 3. Tags first (always independent)
    if delta.DifferentAt("Spec.Tags") {
        if err := rm.syncTags(ctx, desired, latest); err != nil {
            return nil, err
        }
    }

    // 4. Early exit if only tags changed
    if !delta.DifferentExcept("Spec.Tags") {
        return updated, nil
    }

    // 5. Field-specific updates
    if delta.DifferentAt("Spec.FieldA") {
        if err := rm.syncFieldA(ctx, desired, latest); err != nil {
            return nil, err
        }
    }

    // 6. Return updated resource
    return updated, nil
}
```

---

## State Management Patterns

### 3. State-Based Gating

**Frequency:** 70%+ of controllers

Block modifications while resource is in transitional states (CREATING, UPDATING, DELETING).

```go
func (rm *resourceManager) customUpdate(ctx, desired, latest, delta) (*resource, error) {
    // Block during deletion
    if isDeleting(latest) {
        return desired, requeueWaitWhileDeleting
    }

    // Block during creation
    if isCreating(latest) {
        return desired, requeueWaitWhileCreating
    }

    // Block during other updates
    if isUpdating(latest) {
        return desired, requeueWaitWhileUpdating
    }

    // Only proceed if active/available
    if !isActive(latest) {
        return desired, requeueWaitUntilCanModify(latest)
    }

    // Handle terminal/failed states
    if hasTerminalStatus(latest) {
        msg := "Resource is in terminal state"
        ackcondition.SetTerminal(updated, corev1.ConditionTrue, &msg, nil)
        return updated, nil
    }

    // Proceed with updates...
}
```

**State Helper Functions:**
```go
func isActive(r *resource) bool {
    if r.ko.Status.Status == nil {
        return false
    }
    return *r.ko.Status.Status == StatusActive
}

func isDeleting(r *resource) bool {
    if r.ko.Status.Status == nil {
        return false
    }
    return *r.ko.Status.Status == StatusDeleting
}

func hasTerminalStatus(r *resource) bool {
    if r.ko.Status.Status == nil {
        return false
    }
    status := *r.ko.Status.Status
    return status == StatusFailed || status == StatusDeleteFailed
}
```

**Requeue Definitions:**
```go
var (
    requeueWaitWhileDeleting = ackrequeue.NeededAfter(
        errors.New("resource is being deleted"),
        5*time.Second,
    )
    requeueWaitWhileCreating = ackrequeue.NeededAfter(
        errors.New("resource is being created"),
        15*time.Second,
    )
    requeueWaitWhileUpdating = ackrequeue.NeededAfter(
        errors.New("resource is being updated"),
        15*time.Second,
    )
)

func requeueWaitUntilCanModify(r *resource) error {
    return ackrequeue.NeededAfter(
        fmt.Errorf("cannot modify resource in %s state", *r.ko.Status.Status),
        time.Duration(30)*time.Second,
    )
}
```

**Controllers Using This Pattern:**
- EKS: Cluster, Addon, Nodegroup
- DynamoDB: Table
- Kafka: Cluster
- SageMaker: Endpoint
- OpenSearch: Domain
- Lambda: Function (Pending state)

### 4. Async Update with Synced=False

**Frequency:** 30%+ of controllers

For long-running async operations, mark the resource as not synced and requeue.

```go
func returnResourceUpdating(r *resource) (*resource, error) {
    msg := "Resource is currently being updated"
    ackcondition.SetSynced(r, corev1.ConditionFalse, &msg, nil)
    return r, ackrequeue.NeededAfter(
        errors.New("async update in progress"),
        15*time.Second,
    )
}

// Usage in customUpdate:
if delta.DifferentAt("Spec.Logging") {
    if err := rm.updateConfigLogging(ctx, desired); err != nil {
        return nil, err
    }
    return returnResourceUpdating(updated)
}
```

**Controllers Using This Pattern:**
- EKS Cluster (ordered async updates)
- SageMaker Endpoint
- OpenSearch Domain

### 5. Force Recovery on Failed State

**Frequency:** 10%+ of controllers

Trigger an update attempt when resource is in a failed state.

```go
func customPreCompare(delta *ackcompare.Delta, desired, latest *resource) {
    // Force update if resource is in failed state to attempt recovery
    if hasFailedStatus(latest) {
        delta.Add("Spec.ForceRecovery", desired.ko.Status.Status, latest.ko.Status.Status)
    }
}
```

**Controllers Using This Pattern:**
- EKS Addon

---

## Field Update Patterns

### 6. Delta-Based Field Routing

**Frequency:** 65%+ of controllers

Route to specific sync methods based on what changed in the delta.

```go
func (rm *resourceManager) customUpdate(ctx, desired, latest, delta) (*resource, error) {
    // Code vs Configuration separation (Lambda pattern)
    if delta.DifferentAt("Spec.Code.ImageURI") || delta.DifferentAt("Spec.Code.SHA256") {
        if err := rm.updateFunctionCode(ctx, desired, delta, latest); err != nil {
            return nil, err
        }
    }

    if delta.DifferentExcept("Spec.Code", "Spec.Tags", "Spec.ReservedConcurrentExecutions") {
        if err := rm.updateFunctionConfiguration(ctx, desired, delta); err != nil {
            return nil, err
        }
    }

    if delta.DifferentAt("Spec.ReservedConcurrentExecutions") {
        if err := rm.syncConcurrency(ctx, desired); err != nil {
            return nil, err
        }
    }
}
```

**Key Delta Methods:**
- `delta.DifferentAt("Spec.Field")` - Check if specific field changed
- `delta.DifferentExcept("Spec.Field1", "Spec.Field2")` - Check if anything else changed
- `delta.Add("Spec.Field", desired, latest)` - Manually add difference (in PreCompare)

### 7. Independent Field Updates

**Frequency:** 45%+ of controllers

Each field has its own AWS API and can be updated independently.

```go
func (rm *resourceManager) customUpdateRepository(ctx, desired, latest, delta) (*resource, error) {
    var updated *resource = desired

    if delta.DifferentAt("Spec.ImageScanningConfiguration") {
        updated, err = rm.updateImageScanningConfiguration(ctx, updated)
        if err != nil {
            return nil, err
        }
    }
    if delta.DifferentAt("Spec.ImageTagMutability") {
        updated, err = rm.updateImageTagMutability(ctx, updated)
        if err != nil {
            return nil, err
        }
    }
    if delta.DifferentAt("Spec.LifecyclePolicy") {
        updated, err = rm.updateLifecyclePolicy(ctx, updated)
        if err != nil {
            return nil, err
        }
    }
    if delta.DifferentAt("Spec.Policy") {
        updated, err = rm.updateRepositoryPolicy(ctx, updated)
        if err != nil {
            return nil, err
        }
    }
    if delta.DifferentAt("Spec.Tags") {
        err = rm.syncRepositoryTags(ctx, latest, desired)
        if err != nil {
            return nil, err
        }
    }

    return updated, nil
}
```

**Controllers Using This Pattern:**
- ECR Repository
- VPC (CIDR, DNS Support, DNS Hostnames, Security Group Rules)
- Subnet

### 8. Delete-on-Empty Pattern

**Frequency:** 35%+ of controllers

Null or empty value means the field should be deleted/removed.

```go
func (rm *resourceManager) updateLifecyclePolicy(ctx, desired) (*resource, error) {
    dspec := desired.ko.Spec

    // Empty/nil means delete
    if dspec.LifecyclePolicy == nil || *dspec.LifecyclePolicy == "" {
        return rm.deleteLifecyclePolicy(ctx, desired)
    }

    // Otherwise put/update
    input := &svcsdk.PutLifecyclePolicyInput{
        RepositoryName:      dspec.Name,
        LifecyclePolicyText: dspec.LifecyclePolicy,
    }
    _, err := rm.sdkapi.PutLifecyclePolicy(ctx, input)
    return desired, err
}
```

**Controllers Using This Pattern:**
- ECR Repository (LifecyclePolicy, Policy)
- Lambda Function (various configs)
- RDS (optional configurations)

### 9. Set-Based Add/Remove Sync

**Frequency:** 50%+ of controllers

Compute delta between desired and latest sets, then add/remove accordingly.

```go
func (rm *resourceManager) syncManagedPolicies(ctx, desired, latest) error {
    toAdd := []*string{}
    toDelete := []*string{}

    // Find policies to add
    for _, p := range desired.ko.Spec.Policies {
        if !inStringSlice(*p, latest.ko.Spec.Policies) {
            toAdd = append(toAdd, p)
        }
    }

    // Find policies to remove
    for _, p := range latest.ko.Spec.Policies {
        if !inStringSlice(*p, desired.ko.Spec.Policies) {
            toDelete = append(toDelete, p)
        }
    }

    // Delete first (avoid conflicts)
    for _, p := range toDelete {
        if err := rm.detachPolicy(ctx, desired, p); err != nil {
            return err
        }
    }

    // Then add
    for _, p := range toAdd {
        if err := rm.attachPolicy(ctx, desired, p); err != nil {
            return err
        }
    }

    return nil
}
```

**Controllers Using This Pattern:**
- IAM Role (managed policies, inline policies)
- IAM OIDC Provider (client IDs)
- EKS Access Entry (access policies)
- EC2 Security Group (ingress/egress rules)

---

## Sub-Resource Patterns

### 10. Entry-Level CUD (Create/Update/Delete)

**Frequency:** 25%+ of controllers

Sub-resources are managed individually with separate create, update, and delete operations.

```go
func (rm *resourceManager) syncEntries(ctx, desired, latest) error {
    toAdd := []*svcapitypes.Entry{}
    toUpdate := []*svcapitypes.Entry{}
    toDelete := []*svcapitypes.Entry{}

    // Identify entries to add (in desired, not in latest)
    for _, desiredEntry := range desired.ko.Spec.Entries {
        if !containsEntry(latest.ko.Spec.Entries, desiredEntry) {
            toAdd = append(toAdd, desiredEntry)
        }
    }

    // Identify entries to delete (in latest, not in desired)
    for _, latestEntry := range latest.ko.Spec.Entries {
        if !containsEntry(desired.ko.Spec.Entries, latestEntry) {
            toDelete = append(toDelete, latestEntry)
        }
    }

    // Identify entries to update (same key, different values)
    for i, entry := range toAdd {
        for _, latestEntry := range latest.ko.Spec.Entries {
            if sameKey(entry, latestEntry) && !sameValues(entry, latestEntry) {
                toUpdate = append(toUpdate, entry)
                toAdd[i] = nil  // Remove from add list
                break
            }
        }
    }

    // Execute in order: delete, update, add
    for _, entry := range toDelete {
        if err := rm.deleteEntry(ctx, latest, entry); err != nil {
            return err
        }
    }
    for _, entry := range toUpdate {
        if err := rm.replaceEntry(ctx, latest, entry); err != nil {
            return err
        }
    }
    for _, entry := range toAdd {
        if entry == nil {
            continue
        }
        if err := rm.createEntry(ctx, desired, entry); err != nil {
            return err
        }
    }

    return nil
}
```

**Controllers Using This Pattern:**
- EC2 Network ACL (entries)
- EC2 Route Table (routes)
- EC2 Managed Prefix List (entries)
- DynamoDB Table (GSIs - complex variant)

### 11. Association ID Tracking

**Frequency:** 20%+ of controllers

Track association IDs returned on create for later disassociation.

```go
func (rm *resourceManager) syncCIDRBlocks(ctx, desired, latest) error {
    toAdd, toDelete := computeStringDifference(
        desired.ko.Spec.CIDRBlocks,
        latest.ko.Spec.CIDRBlocks,
    )

    // Delete by association ID (not by CIDR value)
    for _, cidr := range toDelete {
        for _, assoc := range latest.ko.Status.CIDRBlockAssociationSet {
            if *cidr == *assoc.CIDRBlock {
                _, err := rm.sdkapi.DisassociateVpcCidrBlock(ctx,
                    &svcsdk.DisassociateVpcCidrBlockInput{
                        AssociationId: assoc.AssociationID,
                    })
                if err != nil {
                    return err
                }
                break
            }
        }
    }

    // Add new CIDRs and capture association IDs
    for _, cidr := range toAdd {
        resp, err := rm.sdkapi.AssociateVpcCidrBlock(ctx,
            &svcsdk.AssociateVpcCidrBlockInput{
                VpcId:     latest.ko.Status.VPCID,
                CidrBlock: cidr,
            })
        if err != nil {
            return err
        }
        // Store association ID in status for future operations
        latest.ko.Status.CIDRBlockAssociationSet = append(
            latest.ko.Status.CIDRBlockAssociationSet,
            &svcapitypes.VPCCIDRBlockAssociation{
                AssociationID: resp.CidrBlockAssociation.AssociationId,
                CIDRBlock:     cidr,
            },
        )
    }

    return nil
}
```

**Controllers Using This Pattern:**
- EC2 VPC (CIDR blocks)
- EC2 DHCP Options (VPC attachment)
- EC2 Internet Gateway (VPC attachment)
- EC2 Network ACL (subnet associations)

### 12. Hierarchical Field Updates

**Frequency:** 15%+ of controllers

Update fields in a specific order due to dependencies.

```go
// DynamoDB Table - ordered update sequence
func (rm *resourceManager) customUpdateTable(ctx, desired, latest, delta) (*resource, error) {
    // 1. Tags (always independent)
    if delta.DifferentAt("Spec.Tags") {
        rm.syncTags(ctx, desired, latest)
    }

    // 2. Resource policy
    if delta.DifferentAt("Spec.ResourcePolicy") {
        rm.syncResourcePolicy(ctx, desired, latest)
    }

    // 3. TTL configuration
    if delta.DifferentAt("Spec.TimeToLive") {
        rm.syncTTL(ctx, desired, latest)
    }

    // 4. SSE specification
    if delta.DifferentAt("Spec.SSESpecification") {
        rm.syncSSE(ctx, desired, latest)
    }

    // 5. Contributor insights
    if delta.DifferentAt("Spec.ContributorInsights") {
        rm.syncContributorInsights(ctx, desired, latest)
    }

    // 6. GSI management (complex - delete, update, add)
    if delta.DifferentAt("Spec.GlobalSecondaryIndexes") {
        rm.syncGSIs(ctx, desired, latest)
    }

    // 7. Provisioned throughput (after GSIs)
    if delta.DifferentAt("Spec.ProvisionedThroughput") {
        rm.syncProvisionedThroughput(ctx, desired, latest)
    }

    // 8. Replicas (last - may require stream)
    if delta.DifferentAt("Spec.Replicas") {
        rm.syncReplicas(ctx, desired, latest)
    }

    return updated, nil
}
```

---

## Comparison Patterns

### 13. Custom PreCompare Normalization

**Frequency:** 40%+ of controllers

Normalize values before delta comparison to avoid false positives.

```go
func customPreCompare(delta *ackcompare.Delta, a, b *resource) {
    // Nil vs empty map normalization
    if a.ko.Spec.Labels == nil && b.ko.Spec.Labels != nil {
        a.ko.Spec.Labels = map[string]*string{}
    } else if a.ko.Spec.Labels != nil && b.ko.Spec.Labels == nil {
        b.ko.Spec.Labels = map[string]*string{}
    }

    // Order-independent list comparison
    if !equalTaintsUnordered(a.ko.Spec.Taints, b.ko.Spec.Taints) {
        delta.Add("Spec.Taints", a.ko.Spec.Taints, b.ko.Spec.Taints)
    }

    // URL normalization (API strips https://)
    if a.ko.Spec.URL != nil && b.ko.Spec.URL != nil {
        aURL := strings.TrimPrefix(*a.ko.Spec.URL, "https://")
        bURL := strings.TrimPrefix(*b.ko.Spec.URL, "https://")
        if aURL != bURL {
            delta.Add("Spec.URL", a.ko.Spec.URL, b.ko.Spec.URL)
        }
    }

    // JSON/Policy document comparison (parse and compare structures)
    if !equalPolicyDocuments(a.ko.Spec.PolicyDocument, b.ko.Spec.PolicyDocument) {
        delta.Add("Spec.PolicyDocument", a.ko.Spec.PolicyDocument, b.ko.Spec.PolicyDocument)
    }
}

// Order-independent comparison helper
func equalTaintsUnordered(a, b []*svcapitypes.Taint) bool {
    if len(a) != len(b) {
        return false
    }
    for _, taintA := range a {
        found := false
        for _, taintB := range b {
            if reflect.DeepEqual(taintA, taintB) {
                found = true
                break
            }
        }
        if !found {
            return false
        }
    }
    return true
}
```

**Common Normalizations:**
- Nil vs empty slice/map
- Order-independent lists
- URL prefix stripping (`https://`)
- JSON/policy document parsing
- Case-insensitive string comparison
- Default value handling

### 14. Custom PostCompare Filtering

**Frequency:** 10%+ of controllers

Remove fields from delta based on annotations or external state.

```go
func customPostCompare(delta *ackcompare.Delta, a, b *resource) {
    // Filter out externally-managed fields
    if isManagedByExternalAutoscaler(a.ko) {
        if delta.DifferentAt("Spec.ScalingConfig.DesiredSize") {
            newDiffs := make([]*ackcompare.Difference, 0)
            for _, d := range delta.Differences {
                if !d.Path.Contains("Spec.ScalingConfig.DesiredSize") {
                    newDiffs = append(newDiffs, d)
                }
            }
            delta.Differences = newDiffs
        }
    }
}

func isManagedByExternalAutoscaler(ko *svcapitypes.Nodegroup) bool {
    if ko.ObjectMeta.Annotations == nil {
        return false
    }
    managedBy, ok := ko.ObjectMeta.Annotations[DesiredSizeManagedByAnnotation]
    return ok && managedBy == "external-autoscaler"
}
```

**Controllers Using This Pattern:**
- EKS Nodegroup (external autoscaler)

### 15. AWS-Managed Resource Filtering

**Frequency:** 15%+ of controllers

Filter out AWS-managed resources from comparison and sync.

```go
// Route Table: Filter local routes and AWS-owned prefix lists
func customPreCompare(delta *ackcompare.Delta, a, b *resource) {
    a.ko.Spec.Routes = removeLocalRoute(a.ko.Spec.Routes)
    b.ko.Spec.Routes = removeLocalRoute(b.ko.Spec.Routes)

    desired, latest := getRoutesDifference(a.ko.Spec.Routes, b.ko.Spec.Routes)
    if len(desired) > 0 || len(latest) > 0 {
        delta.Add("Spec.Routes", a.ko.Spec.Routes, b.ko.Spec.Routes)
    }
}

func removeLocalRoute(routes []*svcapitypes.Route) []*svcapitypes.Route {
    return lo.Filter(routes, func(r *svcapitypes.Route, _ int) bool {
        return r.GatewayID == nil || *r.GatewayID != "local"
    })
}

// Filter AWS-owned prefix lists during sync
func (rm *resourceManager) excludeAWSRoute(ctx, routes) ([]*svcapitypes.Route, error) {
    resp, _ := rm.sdkapi.DescribeManagedPrefixLists(ctx, input)

    awsOwned := make(map[string]bool)
    for _, mpl := range resp.PrefixLists {
        if strings.EqualFold(*mpl.OwnerId, "AWS") {
            awsOwned[*mpl.PrefixListId] = true
        }
    }

    return lo.Filter(routes, func(r *svcapitypes.Route, _ int) bool {
        if r.DestinationPrefixListID == nil {
            return true
        }
        return !awsOwned[*r.DestinationPrefixListID]
    }), nil
}
```

**Controllers Using This Pattern:**
- EC2 Route Table (local routes, AWS prefix lists)
- EC2 Network ACL (default rules #32767)
- EC2 Security Group (default rules)

---

## AWS API Constraint Patterns

### 16. Batch Processing

**Frequency:** 15%+ of controllers

Handle AWS API batch limits by chunking operations.

```go
// Security Group: 1000-rule limit per API call
func (rm *resourceManager) authorizeSecurityGroupIngress(ctx, r, rules) error {
    const batchSize = 1000

    for i := 0; i < len(rules); i += batchSize {
        end := i + batchSize
        if end > len(rules) {
            end = len(rules)
        }

        input := &svcsdk.AuthorizeSecurityGroupIngressInput{
            GroupId:       r.ko.Status.ID,
            IpPermissions: rules[i:end],
        }

        _, err := rm.sdkapi.AuthorizeSecurityGroupIngress(ctx, input)
        rm.metrics.RecordAPICall("CREATE", "AuthorizeSecurityGroupIngress", err)
        if err != nil {
            return err
        }
    }
    return nil
}

// ElastiCache: 20-parameter limit per API call
func (rm *resourceManager) modifyCacheParameterGroup(ctx, desired, params) error {
    const batchSize = 20

    for i := 0; i < len(params); i += batchSize {
        end := i + batchSize
        if end > len(params) {
            end = len(params)
        }

        input := &svcsdk.ModifyCacheParameterGroupInput{
            CacheParameterGroupName: desired.ko.Spec.CacheParameterGroupName,
            ParameterNameValues:     params[i:end],
        }

        _, err := rm.sdkapi.ModifyCacheParameterGroup(ctx, input)
        if err != nil {
            return err
        }
    }
    return nil
}
```

**Known Batch Limits:**
- EC2 Security Group: 1000 rules per call
- ElastiCache Parameters: 20 parameters per call
- RDS Parameters: 20 parameters per call

### 17. One-Attribute-Per-Call

**Frequency:** 10%+ of controllers

AWS API only allows one attribute per modification call.

```go
func (rm *resourceManager) modifyInstanceAttributes(ctx, delta, desired, latest) error {
    input := &svcsdk.ModifyInstanceAttributeInput{
        InstanceId: latest.ko.Status.InstanceID,
    }

    // Mutually exclusive - only ONE attribute per call
    if delta.DifferentAt("Spec.DisableAPITermination") {
        input.DisableApiTermination = &svcsdktypes.AttributeBooleanValue{
            Value: desired.ko.Spec.DisableAPITermination,
        }
    } else if delta.DifferentAt("Spec.InstanceType") {
        input.InstanceType = &svcsdktypes.AttributeValue{
            Value: desired.ko.Spec.InstanceType,
        }
    } else if delta.DifferentAt("Spec.KernelID") {
        input.Kernel = &svcsdktypes.AttributeValue{
            Value: desired.ko.Spec.KernelID,
        }
    } else if delta.DifferentAt("Spec.SecurityGroupIDs") {
        input.Groups = aws.ToStringSlice(desired.ko.Spec.SecurityGroupIDs)
    } else {
        // No more attributes to update
        return nil
    }

    _, err := rm.sdkapi.ModifyInstanceAttribute(ctx, input)
    rm.metrics.RecordAPICall("UPDATE", "ModifyInstanceAttribute", err)
    if err != nil {
        return err
    }

    // Requeue to handle remaining attributes
    return fmt.Errorf("requeuing until all fields are updated")
}
```

**Controllers Using This Pattern:**
- EC2 Instance (ModifyInstanceAttribute)

### 18. Version Management

**Frequency:** 5%+ of controllers

Create new versions instead of in-place updates, with version limit management.

```go
const maxPolicyVersions = 5

func (rm *resourceManager) updatePolicyDocument(ctx, r *resource) (string, error) {
    policyARN := (*string)(r.ko.Status.ACKResourceMetadata.ARN)

    // Ensure we don't exceed version limit
    if err := rm.ensureVersionsLimitNotExceeded(ctx, *policyARN); err != nil {
        return "", err
    }

    // Create new version as default
    input := &svcsdk.CreatePolicyVersionInput{
        PolicyArn:      policyARN,
        PolicyDocument: r.ko.Spec.PolicyDocument,
        SetAsDefault:   aws.Bool(true),
    }

    resp, err := rm.sdkapi.CreatePolicyVersion(ctx, input)
    if err != nil {
        return "", err
    }

    return *resp.PolicyVersion.VersionId, nil
}

func (rm *resourceManager) ensureVersionsLimitNotExceeded(ctx, policyARN string) error {
    versions, err := rm.getPolicyVersions(ctx, policyARN)
    if err != nil {
        return err
    }

    if len(versions) >= maxPolicyVersions {
        // Delete oldest non-default version
        for _, v := range versions {
            if !v.IsDefaultVersion {
                err = rm.deletePolicyVersion(ctx, policyARN, *v.VersionId)
                if err != nil {
                    return err
                }
                break
            }
        }
    }

    return nil
}
```

**Controllers Using This Pattern:**
- IAM Policy (5-version limit)
- EC2 Launch Template (versions)

---

## Pattern Frequency

| Pattern | Frequency | Primary Use Case |
|---------|-----------|------------------|
| Tag Synchronization | 95%+ | All resources with tags |
| State-Based Gating | 70%+ | Async resources |
| Delta-Based Field Routing | 65%+ | Multi-API resources |
| Set-Based Add/Remove | 50%+ | Policies, associations |
| Independent Field Updates | 45%+ | Simple CRUD fields |
| PreCompare Normalization | 40%+ | Complex comparisons |
| Delete-on-Empty | 35%+ | Optional fields |
| Async + Synced=False | 30%+ | Long-running updates |
| Entry-Level CUD | 25%+ | Sub-resources |
| Association ID Tracking | 20%+ | VPC attachments |
| Batch Processing | 15%+ | Large collections |
| AWS-Managed Filtering | 15%+ | Shared resources |
| Hierarchical Updates | 15%+ | Dependent fields |
| One-Attribute-Per-Call | 10%+ | API limitations |
| PostCompare Filtering | 10%+ | External management |
| Version Management | 5%+ | Immutable resources |

---

## Controller-Specific Examples

### High Complexity Controllers

#### DynamoDB Table
- State gating (CREATING, UPDATING, DELETING)
- Hierarchical field updates
- GSI delta computation (add, update, remove)
- Billing mode transitions
- Replica management with stream requirements

#### EKS Cluster
- Ordered async updates (one field at a time)
- Race condition handling (ResourceInUseException)
- Encryption config validation (immutable)
- Synced=False during updates

#### Lambda Function
- Code vs configuration separation
- Multiple update APIs (UpdateFunctionCode, UpdateFunctionConfiguration)
- Concurrency configuration
- Code signing validation

#### EC2 Security Group
- Batch rule processing (1000-rule limit)
- Ingress/egress separation
- Reference resolution checking
- Default rule deletion

#### EC2 Instance
- One-attribute-per-call limitation
- State validation (running/stopped)
- Requeue strategy for multiple fields

### Medium Complexity Controllers

#### IAM Role
- Managed policies sync
- Inline policies sync
- Assume role policy (URL decoding)
- Permission boundary

#### ECR Repository
- Independent field updates
- Delete-on-empty pattern
- Multiple Put/Set APIs

#### VPC
- CIDR block association tracking
- Multiple attribute syncs
- Security group default rules

### Low Complexity Controllers

#### Most simple resources
- Tag sync only
- Single Update API
- No state management

---

## Key Takeaways for Code Generation

1. **Tags are universal** - Generate tag sync automatically for all resources
2. **State checking is critical** - Generate state helpers and requeue patterns
3. **Delta is the routing mechanism** - Use `DifferentAt()` and `DifferentExcept()`
4. **Many resources need multiple APIs** - Single "Update" API is rare
5. **Sub-resource sync is common** - Generate CUD logic for nested items
6. **AWS API quirks require configuration** - Batch limits, one-at-a-time, versions
7. **PreCompare hooks handle normalization** - Nil/empty, order, format differences
8. **Order matters for some updates** - Dependencies between fields
9. **Association IDs must be tracked** - Store in status for disassociation
10. **AWS-managed resources need filtering** - Don't modify system-created items
