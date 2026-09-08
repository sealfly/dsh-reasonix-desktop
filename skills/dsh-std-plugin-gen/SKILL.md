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

## dsh-plugin.json template (Community v0.15 — official schema shape)

> v0.15 官方 schema（vendor/dsh-std/packages/manifest/schema/dsh-plugin-0.15.schema.json）要点：
> 顶层**没有** supports / description / plugin facet；facets 只有 `host`（必填 entry+apiVersion）；
> `$schema` `manifestVersion` `id` `name` `version` `facets` `requires` `permissions`
> `contributes` `subscriptions` 十个顶层字段**全部必填**（后三者可为空数组）；
> id / 命令 id / 权限名必须 namespaced（小写，`.`/`-` 分隔，不含 `/`）；
> 未知顶层字段（含 description、supports、services、provides）→ 直接 rejected。

```json
{
  "$schema": "https://dsh-std.dev/schemas/dsh-plugin-0.15.schema.json",
  "manifestVersion": "0.15",
  "id": "com.example.my-plugin",
  "name": "<Plugin Display Name>",
  "version": "0.1.0",
  "facets": {
    "host": { "entry": "index.js", "apiVersion": "v1alpha1" }
  },
  "requires": {
    "contracts": [
      { "apiVersion": "commands.dsh/v1alpha1", "kind": "Command" }
    ]
  },
  "permissions": [
    { "name": "commands.invoke", "scope": "session", "reason": "invoke commands on the host" }
  ],
  "contributes": {
    "commands": [
      { "id": "com.example.my-plugin.hello", "title": "Hello" }
    ]
  },
  "subscriptions": []
}
```

## Rules (must follow)

1. **id**: reverse-DNS, lowercase letters/digits/hyphens, dot-separated — **no slashes**
   (`com.example.my-plugin`, NOT `com.example/my-plugin`).
2. **manifestVersion**: exactly `0.15` (Community baseline).
3. **version**: valid semver (`x.y.z`).
4. **facets**: v0.15 only has `facets.host` (entry + apiVersion), both **required**;
   there is no `facets.plugin` — a plugin's runnable entry IS `facets.host.entry`
   (a relative path; no drive/absolute prefix, no `..` segments).
5. **requires vs plugin capability semantics**:
   - `requires.contracts` = contracts the plugin NEEDS from the host to function.
     Declare only what you truly consume.
   - The plugin's OWN contributions are declared via `contributes.commands`
     (namespaced id + title), NOT via a supports block (v0.15 has none).
   - optional contract entries must carry a `fallback`.
6. **Registry closure (TUI-PKG-002)**: every `requires.contracts` coordinate must be
   admitted by the target profile's registry (`registry/registry-0.15.json`). Admitted
   coordinates include: `commands.dsh/v1alpha1 Command`, `storage.dsh/v1alpha1
   LocalStorage`, `messages.dsh/v1alpha1 MessageObserver`, `presentation.dsh/v1alpha1
   OpenExternal|UserInteraction|ExternalRedirect`, `tui.dsh/v1alpha1
   DecisionEvents|Channel|SettingsSection|Scene`, `workspace.dsh/v1alpha1
   WorkspaceProvider`. `core.dsh/v1alpha1 Negotiation` lives in the dsh-std core
   negotiation layer and is NOT in the tui profile registry — referencing it there
   makes admission reject with "protocol definition is not admitted by this profile".
   When in doubt, check the registry file of the target profile before finalizing.
7. **permissions**: enumerate concrete capabilities (network scope, filesystem scope,
   shell...) with a human reason. Never claim more than the plugin does. Permission
   names follow the registry's declared permission lists (e.g. `commands.invoke`,
   `storage.local.read`, `messages.observe.read`, `session.input.intercept`).
8. **name kebab-case in package.json**; `main` must point at the real entry.
9. **No collisions**: before finalizing `id`, note it must be unique in the target profile.
   If two plugins claim the same contract slot or command id, admission flags the conflict —
   do not silently produce a second plugin that duplicates an existing id/kind.
10. **Top-level required fields**: `$schema`, `manifestVersion`, `id`, `name`, `version`,
   `facets`, `requires`, `permissions`, `contributes`, `subscriptions` — all ten must be
   present (permissions/contributes/subscriptions may be empty arrays; requires must exist
   even with an empty contracts array).
11. **Do not add** top-level `description`, `supports`, `services`, `provides`, or any
    unknown field — official schema rejects unknown top-level fields.

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
