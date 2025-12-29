# ACK Ecosystem Utilities

This document catalogs the utility packages available in the ACK (AWS Controllers for Kubernetes) ecosystem that are used by generated controller code. Understanding these utilities is essential for code generation.

## Table of Contents

1. [Overview](#overview)
2. [Runtime Package](#runtime-package)
   - [Requeue](#requeue-package)
   - [Condition](#condition-package)
   - [Compare](#compare-package)
   - [Errors](#errors-package)
   - [Types](#types-package)
   - [Util](#util-package)
   - [Tags](#tags-package)
   - [Feature Gates](#feature-gates-package)
3. [Pkg Package](#pkg-package)
   - [Names](#names-package)
   - [FieldPath](#fieldpath-package)
   - [StrUtil](#strutil-package)
4. [Import Aliases](#import-aliases)
5. [Integration Patterns](#integration-patterns)

---

## Overview

The ACK ecosystem consists of two main utility repositories:

| Repository | Import Path | Purpose |
|------------|-------------|---------|
| **runtime** | `github.com/aws-controllers-k8s/runtime` | Core runtime utilities for reconciliation |
| **pkg** | `github.com/aws-controllers-k8s/pkg` | Shared utilities across code generator and controllers |

These packages provide consistent patterns for:
- Resource comparison and delta tracking
- Kubernetes condition management
- Error handling and requeue logic
- Naming conventions
- Tag management

---

## Runtime Package

Import: `github.com/aws-controllers-k8s/runtime`

### Requeue Package

**Location:** `pkg/requeue`
**Import Alias:** `ackrequeue`

Controls how controllers handle reconciliation retries.

#### Types

```go
// NoRequeue - Don't requeue, log the error
type NoRequeue struct {
    err error
}

// RequeueNeeded - Requeue without logging as error
type RequeueNeeded struct {
    err error
}

// RequeueNeededAfter - Requeue after a specified duration
type RequeueNeededAfter struct {
    err      error
    duration time.Duration
}
```

#### Functions

```go
// None creates a no-requeue response (for terminal errors)
func None(err error) *NoRequeue

// Needed creates a requeue response (immediate retry)
func Needed(err error) *RequeueNeeded

// NeededAfter creates a delayed requeue response
func NeededAfter(err error, duration time.Duration) *RequeueNeededAfter
```

#### Usage Examples

```go
// Terminal error - don't retry
return nil, ackrequeue.None(ackerrors.NewTerminalError(err))

// Transient error - retry immediately
return nil, ackrequeue.Needed(err)

// Async operation - retry after delay
return nil, ackrequeue.NeededAfter(
    errors.New("waiting for resource to become active"),
    15*time.Second,
)

// Common requeue durations used in controllers
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
```

---

### Condition Package

**Location:** `pkg/condition`
**Import Alias:** `ackcondition`

Manages Kubernetes Conditions for ACK resources.

#### Condition Types

ACK uses these standard condition types:

| Condition | Purpose |
|-----------|---------|
| `ACK.ResourceSynced` | Resource matches desired state |
| `ACK.Ready` | Resource is ready for use |
| `ACK.Terminal` | Resource is in unrecoverable state |
| `ACK.Recoverable` | Resource can recover from error |
| `ACK.LateInitialized` | Late initialization complete |
| `ACK.ReferencesResolved` | All references resolved |
| `ACK.IAMRoleSelected` | IAM role selection complete |

#### Getter Functions

```go
// Get specific conditions
func Synced(subject acktypes.ConditionManager) *ackv1alpha1.Condition
func Ready(subject acktypes.ConditionManager) *ackv1alpha1.Condition
func Terminal(subject acktypes.ConditionManager) *ackv1alpha1.Condition
func Recoverable(subject acktypes.ConditionManager) *ackv1alpha1.Condition
func LateInitialized(subject acktypes.ConditionManager) *ackv1alpha1.Condition
func ReferencesResolved(subject acktypes.ConditionManager) *ackv1alpha1.Condition
func IAMRoleSelected(subject acktypes.ConditionManager) *ackv1alpha1.Condition

// Get conditions by type
func FirstOfType(subject acktypes.ConditionManager, condType ackv1alpha1.ConditionType) *ackv1alpha1.Condition
func AllOfType(subject acktypes.ConditionManager, condType ackv1alpha1.ConditionType) []*ackv1alpha1.Condition
```

#### Setter Functions

```go
// Set condition with status, message, and reason
func SetSynced(subject acktypes.ConditionManager, status corev1.ConditionStatus, message *string, reason *string)
func SetReady(subject acktypes.ConditionManager, status corev1.ConditionStatus, message *string, reason *string)
func SetTerminal(subject acktypes.ConditionManager, status corev1.ConditionStatus, message *string, reason *string)
func SetRecoverable(subject acktypes.ConditionManager, status corev1.ConditionStatus, message *string, reason *string)
func SetLateInitialized(subject acktypes.ConditionManager, status corev1.ConditionStatus, message *string, reason *string)
func SetReferencesResolved(subject acktypes.ConditionManager, status corev1.ConditionStatus, message *string, reason *string)
func SetIAMRoleSelected(subject acktypes.ConditionManager, status corev1.ConditionStatus, message *string, reason *string)

// Remove conditions
func RemoveReferencesResolved(subject acktypes.ConditionManager)
func Clear(subject acktypes.ConditionManager)
```

#### Helper Functions

```go
// Set references resolved based on error
func WithReferencesResolvedCondition(resource acktypes.ConditionManager, err error)

// Check if late initialization is still in progress
func LateInitializationInProgress(subject acktypes.ConditionManager) bool
```

#### Predefined Messages

```go
const (
    NotManagedMessage    = "Resource already exists and is not managed by ACK"
    NotManagedReason     = "ResourceNotManaged"
    UnknownSyncedMessage = "Unable to determine resource sync state"
    NotSyncedMessage     = "Resource is not synced"
    SyncedMessage        = "Resource synced successfully"
    TerminalMessage      = "Resource is in terminal condition"
    FailedReferenceResolutionMessage = "Reference resolution failed"
    UnavailableIAMRoleMessage = "IAM Role is not available"
)
```

#### Usage Examples

```go
// Mark resource as not synced during async operation
msg := "Resource is currently being updated"
ackcondition.SetSynced(resource, corev1.ConditionFalse, &msg, nil)

// Mark resource as synced
ackcondition.SetSynced(resource, corev1.ConditionTrue, nil, nil)

// Mark resource as terminal (unrecoverable)
msg := "Cannot modify encryption configuration"
reason := "ImmutableField"
ackcondition.SetTerminal(resource, corev1.ConditionTrue, &msg, &reason)

// Check if resource is synced
synced := ackcondition.Synced(resource)
if synced != nil && synced.Status == corev1.ConditionTrue {
    // Resource is synced
}

// Clear all conditions
ackcondition.Clear(resource)
```

---

### Compare Package

**Location:** `pkg/compare`
**Import Alias:** `ackcompare`

Provides utilities for comparing resource states and tracking differences.

#### Delta Type

```go
// Delta contains a list of differences between two resources
type Delta struct {
    Differences []*Difference
}

// Difference represents a single field difference
type Difference struct {
    Path Path        // JSONPath-like field path
    A    interface{} // Value from first resource (desired)
    B    interface{} // Value from second resource (latest)
}

// Path represents a JSONPath-like navigation path
type Path struct {
    parts []string
}
```

#### Delta Functions

```go
// Create new empty delta
func NewDelta() *Delta

// Add a difference to the delta
func (d *Delta) Add(path string, a interface{}, b interface{})

// Check if there's a difference at a specific path
func (d *Delta) DifferentAt(subject string) bool

// Check if there are differences except at specified paths
func (d *Delta) DifferentExcept(exceptPaths ...string) bool
```

#### Path Functions

```go
// Create path from dotted notation
func NewPath(dotted string) Path

// Add part to path
func (p *Path) Push(part string)

// Remove and return last part
func (p *Path) Pop() string

// Check if path contains subject
func (p Path) Contains(subject string) bool

// Get string representation
func (p Path) String() string
```

#### Nil Checking

```go
// Check if nilness differs between two values
func HasNilDifference(a, b interface{}) bool

// Proper nil check for interfaces
func IsNil(i interface{}) bool
func IsNotNil(i interface{}) bool
```

#### Slice Comparison

```go
// Compare string pointer slices (order-independent)
func SliceStringPEqual(a, b []*string) bool

// Compare string slices (order-independent)
func SliceStringEqual(a, b []string) bool
```

#### Map Comparison

```go
// Compare string-to-string-pointer maps
func MapStringStringPEqual(a, b map[string]*string) bool

// Compare string-to-string maps
func MapStringStringEqual(a, b map[string]string) bool
```

#### Secret Reference Comparison

```go
// Compare secret key references
func SecretKeyReferenceEqual(a, b *ackv1alpha1.SecretKeyReference) bool
func SliceSecretKeyReferenceEqual(a, b []*ackv1alpha1.SecretKeyReference) bool

// Find added/removed references
func CompareSecretKeyReferences(a, b []*ackv1alpha1.SecretKeyReference) (equal bool, added, removed []*ackv1alpha1.SecretKeyReference)
```

#### Meta Comparison

```go
// Compare Kubernetes meta objects by JSON marshaling
func MetaV1ObjectEqual(a, b k8smetav1.Object) (bool, error)
```

#### Usage Examples

```go
// Create delta and add differences
delta := ackcompare.NewDelta()
delta.Add("Spec.Tags", desired.ko.Spec.Tags, latest.ko.Spec.Tags)
delta.Add("Spec.Name", desired.ko.Spec.Name, latest.ko.Spec.Name)

// Check specific field
if delta.DifferentAt("Spec.Tags") {
    if err := rm.syncTags(ctx, desired, latest); err != nil {
        return nil, err
    }
}

// Check if anything changed besides tags
if !delta.DifferentExcept("Spec.Tags") {
    // Only tags changed, skip main update API
    return desired, nil
}

// Nil-safe comparison
if ackcompare.HasNilDifference(a.Spec.Field, b.Spec.Field) {
    delta.Add("Spec.Field", a.Spec.Field, b.Spec.Field)
}

// Order-independent slice comparison
if !ackcompare.SliceStringPEqual(a.Spec.Policies, b.Spec.Policies) {
    delta.Add("Spec.Policies", a.Spec.Policies, b.Spec.Policies)
}
```

---

### Errors Package

**Location:** `pkg/errors`
**Import Alias:** `ackerrors`

Defines error types and helpers for ACK-specific scenarios.

#### Error Types

```go
// Terminal error - stops reconciliation permanently
type TerminalError struct {
    err error
}

// Predefined error variables
var (
    AdoptedResourceNotFound      error  // Resource exists but wasn't created by ACK
    ReadOnlyResourceNotFound     error  // Read-only resource not found
    MissingNameIdentifier        error  // Nil name identifier
    NotAdoptable                 error  // Resource cannot be adopted
    NotImplemented               error  // Code path not implemented
    NotFound                     error  // Expected resource not found
    NilResourceManagerFactory    error  // Factory not initialized
    ResourceManagerFactoryNotFound error // Factory lookup failed
    TemporaryOutOfSync           error  // Temporary sync issue
    SecretTypeNotSupported       error  // Only opaque secrets supported
    SecretNotFound               error  // Kubernetes secret not found
)
```

#### Error Functions

```go
// Create terminal error
func NewTerminalError(terminalError error) *TerminalError

// Create ReadOne failure after Create
func NewReadOneFailAfterCreate(numAttempts int) error

// Extract AWS SDK error
func AWSError(err error) (smithy.APIError, bool)

// Extract AWS request failure
func AWSRequestFailure(err error) (smithy.APIError, bool)

// Get HTTP status code from error
func HTTPStatusCode(err error) int
```

#### Resource Reference Errors

```go
// Error variables
var (
    ResourceReferenceOrIDRequired       error
    ResourceReferenceAndIDNotSupported  error
    ResourceReferenceTerminal           error
    ResourceReferenceNotSynced          error
    ResourceReferenceMissingTargetField error
)

// Error constructors with field context
func ResourceReferenceOrIDRequiredFor(fields ...string) error
func ResourceReferenceAndIDNotSupportedFor(fields ...string) error
func ResourceReferenceTerminalFor(resource, namespace, name string) error
func ResourceReferenceNotSyncedFor(resource, namespace, name string) error
func ResourceReferenceMissingTargetFieldFor(resource, namespace, name, targetField string) error
```

#### Field Export Errors

```go
var (
    FieldExportPathDoesNotExist   error
    FieldExportResourceNotSynced  error
    FieldExportInvalidPath        error
    FieldExportQueryFailed        error
    FieldExportMissingConfigMap   error
    FieldExportMissingSecret      error
)
```

#### Usage Examples

```go
// Create terminal error for unrecoverable state
if strings.Contains(awsErr.ErrorMessage(), "cannot be modified") {
    return nil, ackerrors.NewTerminalError(err)
}

// Extract and handle AWS errors
if awsErr, ok := ackerrors.AWSError(err); ok {
    switch awsErr.ErrorCode() {
    case "ResourceNotFoundException":
        return nil, ackerrors.NotFound
    case "ValidationException":
        return nil, ackerrors.NewTerminalError(err)
    case "ResourceInUseException":
        return nil, ackrequeue.NeededAfter(err, 30*time.Second)
    }
}

// Check HTTP status
if ackerrors.HTTPStatusCode(err) == 404 {
    return nil, ackerrors.NotFound
}

// Reference errors
if ref == nil && id == nil {
    return ackerrors.ResourceReferenceOrIDRequiredFor("SecurityGroupID")
}
```

---

### Types Package

**Location:** `pkg/types`
**Import Alias:** `acktypes`

Defines core interfaces for resource management.

#### AWSResource Interface

```go
type AWSResource interface {
    ConditionManager

    // Identifiers returns the AWS resource identifiers
    Identifiers() AWSResourceIdentifiers

    // IsBeingDeleted returns true if deletion timestamp is set
    IsBeingDeleted() bool

    // RuntimeObject returns the underlying Kubernetes runtime object
    RuntimeObject() rtclient.Object

    // MetaObject returns the Kubernetes ObjectMeta
    MetaObject() metav1.Object

    // SetObjectMeta sets the Kubernetes ObjectMeta
    SetObjectMeta(meta metav1.ObjectMeta)

    // SetIdentifiers sets AWS identifiers from adoption
    SetIdentifiers(*ackv1alpha1.AWSIdentifiers) error

    // SetStatus copies status from another resource
    SetStatus(AWSResource)

    // DeepCopy returns a deep copy of the resource
    DeepCopy() AWSResource

    // PopulateResourceFromAnnotation populates fields from annotations
    PopulateResourceFromAnnotation(fields map[string]string) error
}
```

#### ConditionManager Interface

```go
type ConditionManager interface {
    // Conditions returns all conditions
    Conditions() []*ackv1alpha1.Condition

    // ReplaceConditions replaces all conditions
    ReplaceConditions([]*ackv1alpha1.Condition)
}
```

#### AWSResourceManager Interface

```go
type AWSResourceManager interface {
    // ReadOne returns the current state of the resource
    ReadOne(ctx context.Context, res AWSResource) (AWSResource, error)

    // Create creates a new resource
    Create(ctx context.Context, res AWSResource) (AWSResource, error)

    // Update updates an existing resource
    Update(ctx context.Context, desired, latest AWSResource, delta *ackcompare.Delta) (AWSResource, error)

    // Delete deletes the resource
    Delete(ctx context.Context, res AWSResource) (AWSResource, error)

    // LateInitialize sets optional fields from AWS response
    LateInitialize(ctx context.Context, res AWSResource) (AWSResource, error)

    // IsSynced returns whether the resource is synced
    IsSynced(ctx context.Context, res AWSResource) (bool, error)

    // EnsureTags adds required tags to the resource
    EnsureTags(ctx context.Context, res AWSResource, md acktypes.ServiceControllerMetadata) error

    // FilterSystemTags removes system tags from resource
    FilterSystemTags(res AWSResource, tagKeyPrefixes []string) AWSResource

    // ARNFromName constructs ARN from resource name
    ARNFromName(name string) string
}
```

#### AWSResourceIdentifiers Interface

```go
type AWSResourceIdentifiers interface {
    // ARN returns the AWS ARN
    ARN() *ackv1alpha1.AWSResourceName

    // OwnerAccountID returns the AWS account ID
    OwnerAccountID() *ackv1alpha1.AWSAccountID

    // Region returns the AWS region
    Region() *ackv1alpha1.AWSRegion
}
```

---

### Util Package

**Location:** `pkg/util`
**Import Alias:** `ackutil`

General utility functions.

#### Functions

```go
// Check if string is in slice
func InStrings(subject string, collection []string) bool

// Check if string is in pointer slice
func InStringPs(subject string, collection []*string) bool

// Extract host and port from address
func GetHostPort(address string) (string, int, error)
```

#### Usage Examples

```go
// Check membership
if ackutil.InStrings("value", existingValues) {
    // Already exists
}

// Check pointer slice membership
if !ackutil.InStringPs(*policy, attachedPolicies) {
    toAttach = append(toAttach, policy)
}
```

---

### Tags Package

**Location:** `pkg/tags`
**Import Alias:** `acktags`

Manages ACK resource tags.

#### Types

```go
type Tags map[string]string
```

#### Functions

```go
// Create empty tags
func NewTags() Tags

// Merge two tag sets (a takes precedence)
func Merge(a, b Tags) Tags
```

#### Tag Format Constants

```go
const (
    ServiceAliasTagFormat      = "%CONTROLLER_SERVICE%"
    ControllerVersionTagFormat = "%CONTROLLER_VERSION%"
    NamespaceTagFormat         = "%K8S_NAMESPACE%"
    ResourceNameTagFormat      = "%K8S_RESOURCE_NAME%"
    ResourceKindTagFormat      = "%K8S_RESOURCE_KIND%"
    ManagedByTagFormat         = "%MANAGED_BY%"
    KROVersionTagFormat        = "%KRO_VERSION%"
)
```

#### Usage Examples

```go
// Create and merge tags
systemTags := acktags.NewTags()
systemTags["ack.aws/managed"] = "true"
systemTags["ack.aws/controller-version"] = version

allTags := acktags.Merge(userTags, systemTags)
```

---

### Feature Gates Package

**Location:** `pkg/featuregate`
**Import Alias:** `ackfeature`

Manages feature flags for optional ACK functionality.

#### Types

```go
type FeatureStage string // "alpha", "beta", "ga"

type Feature struct {
    Stage   FeatureStage
    Enabled bool
}

type FeatureGates map[string]Feature
```

#### Built-in Features

| Feature | Stage | Default | Description |
|---------|-------|---------|-------------|
| `ResourceAdoption` | Beta | Enabled | Force adoption by annotation |
| `ReadOnlyResources` | Beta | Enabled | Read-only resource annotation |
| `TeamLevelCARM` | Alpha | Disabled | Team-level CARM |
| `ServiceLevelCARM` | Alpha | Disabled | Service-level CARM |
| `IAMRoleSelector` | Alpha | Disabled | IAM role selector |

#### Functions

```go
// Check if feature is enabled
func IsEnabled(name string) bool

// Get feature details
func GetFeature(name string) (Feature, bool)

// List all feature names
func GetFeatureNames() []string

// Get default configuration
func GetDefaultFeatureGates() FeatureGates

// Override defaults
func GetFeatureGatesWithOverrides(overrides map[string]bool) (FeatureGates, error)
```

---

## Pkg Package

Import: `github.com/aws-controllers-k8s/pkg`

### Names Package

**Location:** `names`
**Import:** `github.com/aws-controllers-k8s/pkg/names`

Provides Go-idiomatic naming conventions for AWS API names.

#### Types

```go
type Names struct {
    Original      string  // Original AWS name (e.g., "DBInstanceIdentifier")
    Camel         string  // CamelCase (e.g., "DBInstanceIdentifier")
    CamelLower    string  // camelCase (e.g., "dbInstanceIdentifier")
    Lower         string  // lowercase (e.g., "dbinstanceidentifier")
    Snake         string  // snake_case (e.g., "db_instance_identifier")
    SnakeStripped string  // snake_case without non-alphanumeric
}
```

#### Functions

```go
// Create all name variations
func New(original string) Names
```

#### AWS Initialisms

The package handles 100+ AWS-specific initialisms:

| Input | Camel | CamelLower |
|-------|-------|------------|
| `Id` | `ID` | `id` |
| `Db` | `DB` | `db` |
| `Vpc` | `VPC` | `vpc` |
| `Arn` | `ARN` | `arn` |
| `Iam` | `IAM` | `iam` |
| `Ami` | `AMI` | `ami` |
| `Ec2` | `EC2` | `ec2` |
| `Rds` | `RDS` | `rds` |
| `Eks` | `EKS` | `eks` |
| `Sns` | `SNS` | `sns` |
| `Sqs` | `SQS` | `sqs` |
| `Kms` | `KMS` | `kms` |
| `Ssl` | `SSL` | `ssl` |
| `Tls` | `TLS` | `tls` |
| `Cpu` | `CPU` | `cpu` |
| `Ram` | `RAM` | `ram` |
| `Gpu` | `GPU` | `gpu` |
| `Iops` | `IOPS` | `iops` |
| `Cidr` | `CIDR` | `cidr` |

#### Usage Examples

```go
n := names.New("DBInstanceIdentifier")
fmt.Println(n.Camel)      // "DBInstanceIdentifier"
fmt.Println(n.CamelLower) // "dbInstanceIdentifier"
fmt.Println(n.Snake)      // "db_instance_identifier"
fmt.Println(n.Lower)      // "dbinstanceidentifier"

n = names.New("VpcId")
fmt.Println(n.Camel)      // "VPCID"
fmt.Println(n.CamelLower) // "vpcID"
```

---

### FieldPath Package

**Location:** `path/fieldpath`
**Import:** `github.com/aws-controllers-k8s/pkg/path/fieldpath`

Enhanced path navigation for complex resource structures.

#### Types

```go
type Path struct {
    parts []string
}
```

#### Functions

```go
// Create from dotted notation
func FromString(dotted string) *Path

// String representation
func (p *Path) String() string

// Navigation
func (p *Path) Front() string      // Get first part
func (p *Path) Back() string       // Get last part
func (p *Path) At(index int) string // Get part at index
func (p *Path) Size() int          // Number of parts
func (p *Path) Empty() bool        // Check if empty

// Modification
func (p *Path) Pop() string        // Remove and return last
func (p *Path) PopFront() string   // Remove and return first
func (p *Path) PushBack(part string) // Add to end

// Copying
func (p *Path) Copy() *Path        // Deep copy
func (p *Path) CopyAt(index int) *Path // Copy up to index

// Matching
func (p *Path) HasPrefix(subject string) bool     // Check prefix
func (p *Path) HasPrefixFold(subject string) bool // Case-insensitive prefix
```

#### Usage Examples

```go
path := fieldpath.FromString("Spec.Tags.Environment")

path.Front()  // "Spec"
path.Back()   // "Environment"
path.Size()   // 3
path.At(1)    // "Tags"

path.HasPrefix("Spec")       // true
path.HasPrefix("Status")     // false

// Navigation
path.Pop()    // returns "Environment", path is now "Spec.Tags"
path.PopFront() // returns "Spec", path is now "Tags"

// Building paths
path := fieldpath.FromString("Spec")
path.PushBack("Config")
path.PushBack("Name")
path.String() // "Spec.Config.Name"
```

---

### StrUtil Package

**Location:** `strutil`
**Import:** `github.com/aws-controllers-k8s/pkg/strutil`

String collection utilities.

#### Functions

```go
// Check if string is in slice
func InStrings(subject string, collection []string) bool

// Check if string is in pointer slice
func InStringPs(subject string, collection []*string) bool
```

---

## Import Aliases

Standard import aliases used in generated code:

```go
import (
    // Runtime packages
    ackrequeue   "github.com/aws-controllers-k8s/runtime/pkg/requeue"
    ackcondition "github.com/aws-controllers-k8s/runtime/pkg/condition"
    ackcompare   "github.com/aws-controllers-k8s/runtime/pkg/compare"
    ackerrors    "github.com/aws-controllers-k8s/runtime/pkg/errors"
    acktypes     "github.com/aws-controllers-k8s/runtime/pkg/types"
    ackutil      "github.com/aws-controllers-k8s/runtime/pkg/util"
    acktags      "github.com/aws-controllers-k8s/runtime/pkg/tags"
    ackfeature   "github.com/aws-controllers-k8s/runtime/pkg/featuregate"
    ackrtlog     "github.com/aws-controllers-k8s/runtime/pkg/runtime/log"

    // Pkg packages
    "github.com/aws-controllers-k8s/pkg/names"
    "github.com/aws-controllers-k8s/pkg/path/fieldpath"
)
```

---

## Integration Patterns

### Complete Update Flow Example

Here's how all utilities work together in a typical custom update:

```go
func (rm *resourceManager) customUpdate(
    ctx context.Context,
    desired *resource,
    latest *resource,
    delta *ackcompare.Delta,
) (*resource, error) {
    rlog := ackrtlog.FromContext(ctx)
    exit := rlog.Trace("rm.customUpdate")
    defer func() { exit(err) }()

    // 1. Initialize updated resource
    updated := rm.concreteResource(desired.DeepCopy())
    updated.SetStatus(latest)

    // 2. State-based gating
    if isDeleting(latest) {
        msg := "Resource is being deleted"
        ackcondition.SetSynced(updated, corev1.ConditionFalse, &msg, nil)
        return updated, ackrequeue.NeededAfter(
            errors.New("waiting for deletion"),
            5*time.Second,
        )
    }

    if !isActive(latest) {
        msg := fmt.Sprintf("Resource is in %s state", *latest.ko.Status.Status)
        ackcondition.SetSynced(updated, corev1.ConditionFalse, &msg, nil)

        if hasTerminalStatus(latest) {
            ackcondition.SetTerminal(updated, corev1.ConditionTrue, &msg, nil)
            return updated, nil
        }

        return updated, ackrequeue.NeededAfter(
            errors.New("waiting for resource to become active"),
            15*time.Second,
        )
    }

    // 3. Tag synchronization (always first)
    if delta.DifferentAt("Spec.Tags") {
        if err := rm.syncTags(ctx, desired, latest); err != nil {
            return nil, err
        }
    }

    // 4. Early exit if only tags changed
    if !delta.DifferentExcept("Spec.Tags") {
        return updated, nil
    }

    // 5. Field-specific updates with error handling
    if delta.DifferentAt("Spec.Configuration") {
        if err := rm.updateConfiguration(ctx, desired); err != nil {
            // Handle AWS errors
            if awsErr, ok := ackerrors.AWSError(err); ok {
                switch awsErr.ErrorCode() {
                case "ValidationException":
                    // Terminal error
                    return nil, ackerrors.NewTerminalError(err)
                case "ResourceInUseException":
                    // Race condition - retry
                    return nil, ackrequeue.NeededAfter(err, 30*time.Second)
                }
            }
            return nil, err
        }
    }

    // 6. Async operation handling
    if delta.DifferentAt("Spec.AsyncField") {
        if err := rm.updateAsyncField(ctx, desired); err != nil {
            return nil, err
        }
        msg := "Async update in progress"
        ackcondition.SetSynced(updated, corev1.ConditionFalse, &msg, nil)
        return updated, ackrequeue.NeededAfter(
            errors.New("waiting for async update"),
            30*time.Second,
        )
    }

    // 7. Set-based sync
    if delta.DifferentAt("Spec.Policies") {
        toAdd, toRemove := computePolicyDelta(
            desired.ko.Spec.Policies,
            latest.ko.Spec.Policies,
        )

        // Remove first
        for _, p := range toRemove {
            if err := rm.detachPolicy(ctx, p); err != nil {
                return nil, err
            }
        }

        // Then add
        for _, p := range toAdd {
            if err := rm.attachPolicy(ctx, p); err != nil {
                return nil, err
            }
        }
    }

    return updated, nil
}

// Helper: Compute policy delta
func computePolicyDelta(desired, latest []*string) (toAdd, toRemove []*string) {
    for _, p := range desired {
        if !ackutil.InStringPs(*p, latest) {
            toAdd = append(toAdd, p)
        }
    }
    for _, p := range latest {
        if !ackutil.InStringPs(*p, desired) {
            toRemove = append(toRemove, p)
        }
    }
    return
}
```

### Custom PreCompare Example

```go
func customPreCompare(delta *ackcompare.Delta, a, b *resource) {
    // Nil vs empty normalization
    if a.ko.Spec.Labels == nil && b.ko.Spec.Labels != nil {
        a.ko.Spec.Labels = map[string]*string{}
    }

    // Order-independent comparison
    if !ackcompare.SliceStringPEqual(a.ko.Spec.Policies, b.ko.Spec.Policies) {
        delta.Add("Spec.Policies", a.ko.Spec.Policies, b.ko.Spec.Policies)
    }

    // URL normalization
    if a.ko.Spec.URL != nil && b.ko.Spec.URL != nil {
        aURL := strings.TrimPrefix(*a.ko.Spec.URL, "https://")
        bURL := strings.TrimPrefix(*b.ko.Spec.URL, "https://")
        if aURL != bURL {
            delta.Add("Spec.URL", a.ko.Spec.URL, b.ko.Spec.URL)
        }
    }

    // Force recovery on failed state
    if hasFailedStatus(b) {
        delta.Add("Spec.ForceRecovery", nil, b.ko.Status.Status)
    }
}
```

---

## Summary

These utilities provide the foundation for all ACK controller operations:

| Utility | Purpose | Key Pattern |
|---------|---------|-------------|
| `ackrequeue` | Control retry behavior | Terminal vs transient errors |
| `ackcondition` | Kubernetes conditions | Synced, Ready, Terminal states |
| `ackcompare` | Delta tracking | DifferentAt, DifferentExcept |
| `ackerrors` | Error handling | Terminal errors, AWS error extraction |
| `acktypes` | Core interfaces | AWSResource, AWSResourceManager |
| `ackutil` | String utilities | InStrings, InStringPs |
| `acktags` | Tag management | Merge, system tags |
| `names` | Naming conventions | AWS initialisms, case conversion |
| `fieldpath` | Path navigation | Dotted notation, traversal |

Understanding these utilities is essential for generating correct and idiomatic ACK controller code.
