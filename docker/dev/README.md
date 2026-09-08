# Development audit seccomp profile

`dev-audit-seccomp.json` derives from the [Moby default profile at
61eaf32614c7c71b60bd8927d3e6a4ffc8ff1f31](https://github.com/moby/profiles/blob/61eaf32614c7c71b60bd8927d3e6a4ffc8ff1f31/seccomp/default.json).
The upstream Apache-2.0 license is retained in `MOBY-LICENSE`.

The profile retains the upstream default-deny action and capability/argument
conditions. Three appended rules permit the Bubblewrap launcher to use:

- `mount`, `umount2`, and `pivot_root` for its filesystem namespace;
- `unshare` with only user and mount namespace flags;
- `clone` with user/mount namespace flags while retaining the other namespace
  restrictions (on the development image's amd64 and arm64 architectures).

Kernel capability checks still apply. The container receives no additional
capabilities and does not run privileged. These exceptions apply to the opted-in
development container; Bubblewrap separately confines each audit model's writes.
Ordinary sandbox invocations use Docker's default profile.

When updating the upstream snapshot, review the syscall delta and rerun the real
Linux confinement tests and Docker smoke. Do not replace this profile with
`seccomp=unconfined` when a host rejects namespace setup; retain the diagnostic
failure instead.
