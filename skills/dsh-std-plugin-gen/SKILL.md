---
name: dsh-std-plugin-gen
description: Generate a dsh-std compliant DSH plugin scaffold (dsh-plugin.json Community v0.15 + facets + skeleton). Use when the user asks to create/make/generate a DSH plugin, extension, or harness plugin.
whenToUse: user asks to create or scaffold a DSH plugin / extension; or asks what makes a plugin dsh-std compliant
---

# dsh-std Plugin Generator

You are a generator that produces **dsh-std compliant** DSH plugins. The plugin must pass
`DshStdParseManifest` + `AdmitPlugin` (Community v0.15 five-state admission) used by the
DSH-ReasonixUI desktop host and the DSH ecosystem.

## Output layout

Create a plugin directory with at least:

```
<plugin-id>/
  dsh-plugin.json      # REQUIRED manifest (Community v0.15)
  package.json         # npm metadata (name/version/main)
  index.js             # entry (Cordis plugin apply(ctx, config))
  README.md            # optional
```

## dsh-plugin.json template (Community v0.15)

```json
{
  "$schema": "https://dsh-std.dev/schemas/dsh-plugin-0.15.schema.json",
  "manifestVersion": "0.15",
  "id": "com.example/<plugin-id>",
  "name": "<Plugin Display Name>",
  "version": "0.1.0",
  "description": "One-line description.",
  "facets": {
    "host":     { "entry": "<executable-or-empty>", "apiVersion": "host.dsh/v1alpha1" },
    "plugin":   { "entry": "index.js", "apiVersion": "plugin.dsh/v1alpha1" }
  },
  "requires": {
    "contracts": [
      { "apiVersion": "core.dsh/v1alpha1", "kind": "Negotiation", "optional": false }
    ]
  },
  "supports": {
    "contracts": [
      { "apiVersion": "tool.dsh/v1", "kind": "Tool" }
    ]
  },
  "permissions": [
    { "name": "network", "scope": "127.0.0.1:3080", "reason": "why it needs this" }
  ]
}
```

## Rules (must follow)

1. **id**: reverse-DNS, lowercase letters/digits/hyphens, dot-separated (`com.example.my-plugin`).
2. **manifestVersion**: exactly `0.15` (Community baseline).
3. **version**: valid semver (`x.y.z`).
4. **facets**: declare every surface the plugin mounts. A plugin exposes `plugin.dsh/v1alpha1`;
   a host exposes `host.dsh/v1alpha1`.
5. **requires vs supports** semantics:
   - `requires` = contracts the plugin NEEDS from the host to function (core Negotiation is
     always required). Declare only what you truly consume (connection, session, tool...).
   - `supports` = contracts the plugin PROVIDES. Pick from the host vocabulary:
     `command.dsh/v1 CommandRuntime`, `tool.dsh/v1 Tool`, `model.dsh/v1 ModelCatalog`,
     `presentation.dsh/v1alpha1 Presentation`, `manifest.dsh/v1alpha1 Manifest`.
6. **permissions**: enumerate concrete capabilities (network scope, filesystem scope, shell...)
   with a human reason. Never claim more than the plugin does.
7. **name kebab-case in package.json**; `main` must point at the real entry.
8. **No collisions**: before finalizing `id`, note it must be unique in the target profile.
   If two plugins claim the same contract slot, admission flags the conflict — do not
   silently produce a second plugin that duplicates an existing id/kind.

## index.js skeleton (Cordis)

```js
module.exports = {
  name: "plugin-id",
  apply(ctx) {
    // mount your hooks/services here
    ctx.logger?.info("plugin loaded");
  },
};
```

## After generating

- Run the manifest through `DshStdParseManifest` / `DshStdAdmit` (the desktop client exposes
  these under window.go.main.App) and report the admission state.
- Tell the user how to install: `dsh plugin --profile web add <local-path-or-spec>`.
- If admission returns a non-`compatible` state, fix the manifest field it flags and retry.
