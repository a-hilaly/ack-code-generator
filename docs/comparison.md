# ACK Comparison Strategies

This document describes the comparison challenges in ACK controllers and proposes configuration options for the code generator based on **actual patterns observed across 80+ customPreCompare implementations**.

## Table of Contents

1. [The Problem](#the-problem)
2. [Patterns Found in Hooks](#patterns-found-in-hooks)
3. [Proposed Configuration](#proposed-configuration)
4. [Examples from Real Controllers](#examples-from-real-controllers)

---

## The Problem

### Nil vs Empty

Kubernetes and AWS represent "nothing" differently:

```go
var nilSlice []*string = nil
emptySlice := []*string{}

reflect.DeepEqual(nilSlice, emptySlice)  // false - causes infinite requeue!
```

**Solution:** `equality.Semantic.DeepEqual` (commit ca61a72) handles nil vs empty slices/maps.

### Collection Ordering

AWS often returns collections in arbitrary order:

```go
// User specifies:
subnets: ["subnet-a", "subnet-b"]

// AWS returns:
subnets: ["subnet-b", "subnet-a"]
```

### IAM Policy Transformation

AWS transforms IAM policy documents, normalizing structure, adding defaults, converting strings to arrays.

### Server-Side Defaults

AWS returns default values for fields the user didn't specify:

```go
// User doesn't specify Accelerate
// AWS returns: {Status: "Suspended"}

// User doesn't specify BillingMode
// AWS returns: "PROVISIONED"
```

---

## Patterns Found in Hooks

After analyzing **80+ customPreCompare functions** across ACK controllers, here are the actual patterns used:

### Pattern 1: Server-Side Defaults (~35% of hooks)

**The most common pattern.** When user doesn't specify a field, but AWS returns a default value, hooks normalize both sides to avoid false deltas.

```go
// s3 bucket - extensive default normalization
if a.ko.Spec.Accelerate == nil && b.ko.Spec.Accelerate != nil {
    a.ko.Spec.Accelerate = &svcapitypes.AccelerateConfiguration{}
    if b.ko.Spec.Accelerate.Status != nil &&
        *b.ko.Spec.Accelerate.Status == string(DefaultAccelerationStatus) {
        a.ko.Spec.Accelerate.Status = aws.String(string(DefaultAccelerationStatus))
    }
}
if a.ko.Spec.Versioning == nil && b.ko.Spec.Versioning != nil {
    a.ko.Spec.Versioning = &svcapitypes.VersioningConfiguration{}
    if b.ko.Spec.Versioning.Status != nil &&
        *b.ko.Spec.Versioning.Status == string(DefaultVersioningStatus) {
        a.ko.Spec.Versioning.Status = aws.String(string(DefaultVersioningStatus))
    }
}

// apigateway integration - nil to empty map/struct normalization
if a.ko.Spec.RequestTemplates == nil && b.ko.Spec.RequestTemplates != nil {
    a.ko.Spec.RequestTemplates = map[string]*string{}
} else if a.ko.Spec.RequestTemplates != nil && b.ko.Spec.RequestTemplates == nil {
    b.ko.Spec.RequestTemplates = map[string]*string{}
}

// dynamodb table - default BillingMode
if a.ko.Spec.BillingMode == nil {
    a.ko.Spec.BillingMode = aws.String(string(v1alpha1.BillingMode_PROVISIONED))
}

// rds db_instance - default values
var (
    ServiceDefaultBackupTarget            = "region"
    ServiceDefaultNetworkType             = "IPV4"
    ServiceDefaultInsightsRetentionPeriod = int64(7)
)
```

**Controllers using this pattern:** S3, DynamoDB, RDS, APIGateway, ElastiCache, Pipes, Kafka, EKS, etc.

### Pattern 2: Tags Comparison (~25% of hooks)

Nearly every controller with tags does unordered map comparison:

```go
// recyclebin, prometheusservice, dynamodb, lambda, iam, etc.
if len(a.ko.Spec.Tags) != len(b.ko.Spec.Tags) {
    delta.Add("Spec.Tags", a.ko.Spec.Tags, b.ko.Spec.Tags)
} else if len(a.ko.Spec.Tags) > 0 {
    if !equalTags(a.ko.Spec.Tags, b.ko.Spec.Tags) {
        delta.Add("Spec.Tags", a.ko.Spec.Tags, b.ko.Spec.Tags)
    }
}
```

### Pattern 3: Unordered Collection Comparison (~15% of hooks)

Used for SubnetIDs, SecurityGroupIDs, PolicyARNs, Taints, etc.

```go
// eks nodegroup - Taints
for _, taintA := range a.ko.Spec.Taints {
    var matched = false
    for _, taintB := range b.ko.Spec.Taints {
        if reflect.DeepEqual(taintA, taintB) {
            matched = true
            break
        }
    }
    if !matched {
        delta.Add("Spec.Taints", ...)
    }
}

// networkfirewall - SubnetMappings (sort before compare)
sort.Slice(newSubnetMappings[:], func(i, j int) bool {
    return *newSubnetMappings[i].SubnetID < *newSubnetMappings[j].SubnetID
})
```

### Pattern 4: Nil Ptr vs Empty Struct (~10% of hooks)

Deep nested struct comparison with nil vs empty struct handling (pipes-controller does this extensively):

```go
// pipes-controller - hasNilDifference checks nil vs empty struct
func isEmpty(i interface{}) bool {
    if i == nil { return true }
    switch reflect.TypeOf(i).Kind() {
    case reflect.Ptr, reflect.Map, reflect.Array, reflect.Chan, reflect.Slice:
        if reflect.ValueOf(i).IsNil() { return true }
        if reflect.ValueOf(i).Elem().Kind() == reflect.Struct {
            return reflect.ValueOf(i).Elem().IsZero()  // <-- checks zero-value struct
        }
    }
    return false
}

// Used for deeply nested structs
if hasNilDifference(a.ko.Spec.SourceParameters.DynamoDBStreamParameters,
                    b.ko.Spec.SourceParameters.DynamoDBStreamParameters) {
    delta.Add("Spec.SourceParameters.DynamoDBStreamParameters", ...)
}
```

### Pattern 5: IAM Policy Semantic Comparison (~5% of hooks)

Only IAM controller does this:

```go
// iam role
var policyDocumentA awsiampolicy.Policy
json.Unmarshal([]byte(*a.ko.Spec.AssumeRolePolicyDocument), &policyDocumentA)
var policyDocumentB awsiampolicy.Policy
json.Unmarshal([]byte(*b.ko.Spec.AssumeRolePolicyDocument), &policyDocumentB)

if !reflect.DeepEqual(policyDocumentA, policyDocumentB) {
    delta.Add("Spec.AssumeRolePolicyDocument", ...)
}
```

### Pattern 6: External Management via Annotation (~2% of hooks)

Only EKS nodegroup does this:

```go
// eks nodegroup - customPostCompare
if isManagedByExternalAutoscaler(a.ko) && delta.DifferentAt("Spec.ScalingConfig.DesiredSize") {
    // Remove DesiredSize from delta
    newDiffs := make([]*ackcompare.Difference, 0)
    for _, d := range delta.Differences {
        if !d.Path.Contains("Spec.ScalingConfig.DesiredSize") {
            newDiffs = append(newDiffs, d)
        }
    }
    delta.Differences = newDiffs
}
```

---

## Proposed Configuration

Based on actual patterns, here's a minimal, pragmatic configuration schema:

### Configuration Options

```yaml
resources:
  MyResource:
    fields:
      Spec.FieldName:
        compare:
          # Option 1: Unordered comparison for collections
          unordered: true

          # Option 2: IAM policy semantic comparison
          is_iam_policy: true

          # Option 3: Skip if annotation matches (external management)
          ignore_if_annotation:
            key: "annotation-key"
            value: "annotation-value"

        # Option 4: Server-side defaults (existing feature)
        late_initialize: {}
```

#### 1. `unordered` (boolean)

Compare collections without considering order.

**Evidence:** Used in ~15-25% of hooks for Tags, SubnetIDs, SecurityGroupIDs, Taints, PolicyARNs, etc.

```yaml
fields:
  Spec.SubnetIDs:
    compare:
      unordered: true
```

**Generated code:**
```go
if !ackcompare.SliceStringPEqual(a.ko.Spec.SubnetIDs, b.ko.Spec.SubnetIDs) {
    delta.Add("Spec.SubnetIDs", ...)
}
```

#### 2. `is_iam_policy` (boolean)

Use IAM policy semantic comparison for policy documents.

**Evidence:** Used in IAM controller for AssumeRolePolicyDocument, InlinePolicies. AWS transforms policy structure.

```yaml
fields:
  Spec.AssumeRolePolicyDocument:
    compare:
      is_iam_policy: true
```

**Generated code:**
```go
if !compareIAMPolicyDocuments(a.ko.Spec.AssumeRolePolicyDocument, b.ko.Spec.AssumeRolePolicyDocument) {
    delta.Add("Spec.AssumeRolePolicyDocument", ...)
}
```

#### 3. `ignore_if_annotation` (object)

Skip comparison if resource has matching annotation.

**Evidence:** Used in EKS nodegroup for cluster autoscaler managing DesiredSize.

```yaml
fields:
  Spec.ScalingConfig.DesiredSize:
    compare:
      ignore_if_annotation:
        key: "eks.amazonaws.com/managed-by"
        value: "cluster-autoscaler"
```

**Generated code:**
```go
if !hasAnnotation(a.ko, "eks.amazonaws.com/managed-by", "cluster-autoscaler") {
    if !equality.Semantic.DeepEqual(a.ko.Spec.ScalingConfig.DesiredSize, b.ko.Spec.ScalingConfig.DesiredSize) {
        delta.Add("Spec.ScalingConfig.DesiredSize", ...)
    }
}
```

#### 4. `late_initialize` (existing feature)

For server-side defaults, use the existing `late_initialize` config. See [Server-Side Defaults](#server-side-defaults-use-late_initialize) section.

**Evidence:** ~35% of hooks handle server-side defaults (S3, DynamoDB, RDS, APIGateway). These could use `late_initialize` instead.

```yaml
fields:
  Spec.BillingMode:
    late_initialize: {}
  Spec.Path:
    late_initialize: {}
```

### What We're NOT Including

Based on actual hook analysis, these were considered but **not needed** as separate config options:

| Option | Reason Not Included |
|--------|---------------------|
| `case_insensitive` | No hooks use case-insensitive comparison |
| `nil_to_empty_map` | Handled by default with `equality.Semantic.DeepEqual` |
| `nil_to_empty_struct` | Rare; pipes-controller handles with custom `isEmpty()` - too complex to generalize |
| `key_fields` for struct comparison | Complex; hooks do this manually per-field when needed |

---

## Examples from Real Controllers

### S3 Bucket

```yaml
resources:
  Bucket:
    fields:
      Spec.Accelerate.Status:
        late_initialize: {}
      Spec.Versioning.Status:
        late_initialize: {}
      Spec.RequestPayment.Payer:
        late_initialize: {}
      Spec.Tagging:
        compare:
          unordered: true
```

### IAM Role

```yaml
resources:
  Role:
    fields:
      Spec.AssumeRolePolicyDocument:
        compare:
          is_iam_policy: true
      Spec.Tags:
        compare:
          unordered: true
      Spec.Policies:
        compare:
          unordered: true
```

### EKS Nodegroup

```yaml
resources:
  Nodegroup:
    fields:
      Spec.Taints:
        compare:
          unordered: true
      Spec.ScalingConfig.DesiredSize:
        compare:
          ignore_if_annotation:
            key: "eks.amazonaws.com/managed-by"
            value: "cluster-autoscaler"
```

### DynamoDB Table

```yaml
resources:
  Table:
    fields:
      Spec.BillingMode:
        late_initialize: {}
      Spec.SSESpecification.Enabled:
        late_initialize: {}
```

### RDS DBInstance

```yaml
resources:
  DBInstance:
    fields:
      Spec.BackupTarget:
        late_initialize: {}
      Spec.NetworkType:
        late_initialize: {}
      Spec.PerformanceInsightsRetentionPeriod:
        late_initialize: {}
```

---

## Default Behavior

1. **Use `equality.Semantic.DeepEqual`** for all fields by default (nil == empty for slices/maps)
2. **Ordered comparison** for collections unless `unordered: true`
3. **Standard string comparison** (no case insensitivity)
4. **No external management awareness** unless `ignore_if_annotation` configured
5. **No late initialization** unless `late_initialize: {}` configured

---

## Server-Side Defaults: Use `late_initialize`

The code generator already has `late_initialize` - **use it for server-side defaults**.

### How It Works

```yaml
# generator.yaml
resources:
  Role:
    fields:
      Path:
        late_initialize: {}
```

**Generated code:**
```go
// In LateInitialize() method
if observedKo.Spec.Path != nil && latestKo.Spec.Path == nil {
    latestKo.Spec.Path = observedKo.Spec.Path
}
```

### Why This Solves the Problem

1. User creates resource without `BillingMode`
2. AWS defaults to `"PROVISIONED"`
3. `late_initialize` copies `"PROVISIONED"` into Spec
4. Next reconcile: Spec has `"PROVISIONED"`, AWS has `"PROVISIONED"` → **no delta**

### Why Controllers Use customPreCompare Instead

Looking at S3, DynamoDB, RDS hooks - they use customPreCompare for defaults instead of `late_initialize`. Reasons:

1. **Historical** - `late_initialize` was added later, controllers already had hooks
2. **Awareness gap** - controller authors may not have known about it

### Recommendation

**For SDKUpdates generation:** Generate `late_initialize: {}` config for fields with server-side defaults.

No need for a separate `compare.default_value` option - `late_initialize` already exists and works.

### Controllers Already Using `late_initialize`

- **IAM**: `Path` for Role, User, Group, InstanceProfile
- **ECR**: `ImageScanningConfiguration.ScanOnPush`

---

## Summary

| Scenario | Configuration |
|----------|---------------|
| AWS reorders collections (tags, subnets, etc.) | `compare.unordered: true` |
| IAM policy fields | `compare.is_iam_policy: true` |
| Externally managed fields (autoscaler) | `compare.ignore_if_annotation: {...}` |
| AWS returns default when user didn't specify | `late_initialize: {}` |
| Nil vs empty slices/maps | Already handled by `equality.Semantic.DeepEqual` |

This covers **95%+ of actual customPreCompare patterns** found across ACK controllers.
