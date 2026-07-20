# Model-family and Variant-aware SOP Design

**Date:** 2026-07-17  
**Status:** Approved design, pending implementation plan

## Goal

Allow CargoFlows to reuse capture instructions and structural image references across products in the same model family while preventing a reference variant's color, texture, labels, ports, controls, accessories, or packaging from leaking into the target SKU's generated content.

The first supported workflow assumes that every target SKU has at least one approved identity-anchor image. Images from another SKU may supplement structure and viewpoint evidence, but may never replace target-variant identity evidence.

## Non-goals

- Generating a target SKU solely from a color name and another SKU's images.
- Automatically deciding that products belong to the same model family.
- Automatically discovering variant differences without operator confirmation.
- Automatically publishing generated images without human review.
- Migrating old development SOP, AI, or image data. Data may be re-uploaded after release.

## Core Safety Invariant

No target SKU identity anchor means no final product-image generation.

Cross-SKU images can establish only explicitly allowed geometry, viewpoint, or unchanged detail facts. They cannot establish target color, finish, texture, logos, labels, ports, controls, accessories, package contents, or packaging. When evidence conflicts, approved target-SKU evidence always wins.

## Domain Model

### ModelFamily

`ModelFamily` groups related SKUs across different `Product` rows. Membership is explicit, versioned, and operator-managed rather than inferred from product names.

Fields include:

- public ID, brand, family name, and model code;
- localized common-structure description;
- versioned invariant definitions, such as common outline or unchanged button layout;
- allowed variation dimensions, such as color, material, finish, texture, trim, ports, labels, accessories, and packaging;
- lifecycle status and audit metadata.

`ModelFamilyMember` links a SKU to one family and records when, why, and by whom it was added. A SKU may have only one active family membership in the initial implementation.

### VariantIdentityManifest

Each family member has a published, immutable `VariantIdentityManifest` version. It describes the exact target SKU and becomes authoritative structured evidence for generation.

The manifest supports:

- named colors and optional color values for multiple regions;
- material, finish, and texture;
- logos, labels, and printed text;
- ports, buttons, cut-outs, and other controls;
- accessory and packaging variants;
- additional typed distinguishing features;
- the set of attributes that must be proven by target-SKU images;
- localized operator notes.

Published manifests are immutable. Changes create a new version. AI jobs freeze the selected published version and never follow later updates.

### VariantDifferenceRegion

A manifest may define regions whose appearance or geometry differs from other family members. Each region contains:

- a typed difference kind;
- localized description;
- optional normalized rectangle or polygon coordinates;
- linked approved target-SKU evidence assets;
- strictness: `exact`, `preserve`, or `descriptive`;
- attributes forbidden from cross-SKU inheritance;
- required capture view keys.

An `exact` region must have approved target evidence before image generation. Operators may add requirements but cannot waive an automatically required exact-evidence rule.

### ModelFamilyReferenceAsset

Existing `Asset` rows remain owned by their source SKU. `ModelFamilyReferenceAsset` grants narrowly scoped reuse without copying or changing ownership.

Each grant records:

- source asset and source SKU;
- family and SOP view key;
- role: `geometry_only`, `viewpoint_only`, or `detail_geometry`;
- explicitly allowed attributes or regions;
- explicitly forbidden attributes or regions;
- reviewer, approval state, version, and audit metadata.

Cross-SKU `appearance` reuse is forbidden by default. A reference cannot be used outside its family or beyond its approved role.

## SOP Layering

Capture requirements resolve through three layers:

1. category base SOP;
2. model-family SOP;
3. target-SKU variant capture requirements.

The resolved result is frozen for a photo session and later frozen again as provenance in an AI job snapshot.

### Category base SOP

The base defines reusable coordinate, background, quality, framing, and common view requirements.

### Model-family SOP

A published family SOP version locks a specific published category SOP version. It may override or extend views using stable `view_key` values, not database IDs. It may make views required or optional and declare which views permit family references.

### Variant capture requirements

The published identity manifest and its difference regions derive additional capture requirements:

- color changes require a target-SKU full-product identity view;
- texture or finish changes require a surface detail;
- port or control changes require the corresponding side/detail view;
- label changes require a readable label detail;
- packaging changes require target packaging views.

The resolver preserves per-layer provenance and emits a deterministic `ResolvedSOP` hash. Published inputs are immutable and a resolved snapshot never changes silently.

## Evidence Matrix and Preflight

Before creating any billable image turn, CargoFlows builds a property-to-evidence matrix:

| Property | Required evidence |
| --- | --- |
| Color, texture, logo, label | Target SKU identity assets |
| Ports, controls, variant geometry | Target SKU region/detail assets |
| Explicit family invariants | Approved family structural references |
| Viewpoint and composition | Resolved SOP |
| Claims and marketing copy | Target SKU structured data |

Preflight validates family membership, published versions, asset approval, evidence coverage, reference grants, image integrity, and snapshot freshness. A correctable failure blocks only image generation and incurs no OpenAI call. Title and SEO generation may continue using target-SKU structured data.

Public preflight reason codes are:

- `model_family_missing`;
- `family_sop_not_published`;
- `identity_manifest_not_published`;
- `missing_identity_anchor`;
- `difference_region_not_covered`;
- `reference_not_approved`;
- `reference_cross_family`;
- `reference_usage_forbidden`;
- `snapshot_stale`.

After correction, users create a new task or turn. The server does not silently replace frozen inputs.

## Cross-SKU Reference Derivatives

The worker does not send unrestricted cross-SKU originals to OpenAI by default. It creates an immutable AI-only derivative according to the reference grant:

- `geometry_only` references become grayscale;
- forbidden color, label, port, control, and other difference regions are masked or weakened;
- permitted structure and perspective remain visible;
- the original remains available to authorized human reviewers;
- derivative transform version, source hash, output hash, and allowed-use metadata are audited.

The derivative is stored in private object storage and is never publicly readable. Internal object keys and signed URLs do not enter prompts or audit metadata.

## Prompt and Provider Input

The image prompt compiler adds a fixed evidence-priority policy to L0/L1:

- target-SKU identity evidence outranks all family references;
- cross-SKU references cannot prove variant appearance or unlisted structure;
- conflicting reference attributes must be ignored;
- variants must not be averaged, blended, or substituted;
- only declared family invariants may transfer;
- lower-level template and user prompts cannot override identity rules.

Every provider input receives an explicit role label. Initial-generation ordering is:

1. target identity anchor;
2. target difference-region details;
3. restricted family structural derivatives;
4. SOP composition references.

Edit ordering is:

1. selected generated parent;
2. target identity anchor;
3. target difference-region details;
4. restricted family structural derivatives;
5. SOP composition references.

Restart excludes all generated parents. The provider adapter interleaves trusted server-generated role text with each image block so that ordering and permissions are explicit.

Persisted non-binary input descriptors include source public reference, source SKU public reference, input role, allowed and forbidden attributes, covered difference regions, SOP view, transform version, and SHA-256. They exclude bytes, API keys, internal object locators, credentials, endpoints, and signed URLs.

## Multi-turn History and Review

Every generated candidate remains immutable and visible in its image thread. Editing may branch from any candidate; restart creates a new root from the original frozen evidence. Selection only changes an audited pointer.

Generated images enter manual review. The Web review surface presents:

- target identity and region evidence;
- generated result;
- family references marked as structure-only;
- a checklist for color, texture, logos, labels, ports, controls, packaging, mirroring, and unsupported additions.

Reviewers may accept, reject, select, edit, or restart. Rejected images and failed turns remain in chronological history. No generated result automatically replaces source assets or publishes to a marketplace.

## Permissions

- Administrators manage families, variation dimensions, family SOP publication, and cross-SKU reference policy.
- Operators manage membership, publish identity manifests, annotate regions, approve reference grants, create AI tasks, and review results.
- Photographers execute resolved capture requirements and upload evidence but cannot change family membership or identity definitions.
- Viewers have read-only access and cannot generate, edit, select, approve, or publish.

Family membership changes, manifest publication, reference grants, and inherited-attribute changes require audit events.

## Audit Requirements

Each generation or edit records:

- target SKU, family, and frozen version identifiers;
- resolved SOP and all source versions;
- identity manifest and difference-region versions;
- every input role, source SKU, and content hash;
- allowed and forbidden inherited attributes;
- derivative transform and hash;
- prompt layer versions and compiled hash;
- provider response, request, and image-call IDs;
- token usage, cost, actor, parent, turn, and selection history.

Audit data must never contain image bytes, decrypted API keys, internal object keys, signed URLs, provider response bodies, or unsafe internal errors.

## Concurrency and Failure Handling

Family, manifest, SOP, and reference versions are locked when the task snapshot is created. Later edits do not affect an active or historical job. Concurrent publication either finishes before snapshot creation or yields a conflict; it cannot produce a mixed-version snapshot.

Missing evidence, invalid references, and stale draft selection are user-correctable preflight failures. Moderation and invalid requests are not automatically retried. Ambiguous provider timeouts or 5xx outcomes enter `needs_attention` to prevent duplicate billing. Storage and database recovery follow the existing per-candidate idempotency design.

## Acceptance Tests

The implementation must prove:

- a different-color family reference cannot establish target color;
- a changed port requires target port evidence;
- different labels or logos cause the reference region to be masked;
- package differences prevent packaging reuse while allowing approved product-body geometry reuse;
- target evidence wins every conflict;
- cross-family references fail in both service and persistence layers;
- edit reuses the task's frozen evidence, not later manifest changes;
- restart excludes generated parents;
- unselected suite slots cause zero provider calls and zero cost;
- rejected and superseded images remain visible;
- missing identity evidence blocks image calls but not eligible title/SEO work;
- concurrent family or manifest publication cannot drift a frozen task;
- private derivatives cannot be fetched anonymously;
- no prompt, API response, log, or audit row leaks secrets or internal locators.

The final E2E path creates a family with two cross-product SKUs, publishes a color/detail difference manifest, captures target anchors, grants a structure-only reference, generates and edits an image, confirms all history remains visible, and verifies the fake provider received only correctly labeled target images and restricted derivatives.
