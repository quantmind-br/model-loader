# Retired Orca IQ3_XXS example configs

These five JSON files were moved byte-for-byte from
`docs/examples/strata-orca-iq3-xxs/`. They describe the 2026-10-01 local dual-GPU
mmap and single-GPU resident experiments, not portable or current defaults.
The checkpoint, prepared packs, live profiles and `strata-fork` catalog entry
were subsequently retired. Absolute paths and backend IDs intentionally retain
historical values; do not import these examples into the current profile store.

- [Mmap profile](profile.json) and [engine config](engine.json).
- [Resident profile](profile-resident.json) and [engine config](engine-resident.json).
- [Shared settings](shared-settings.json).
- [Original setup record](../strata-orca-iq3-xxs-profile-2026-10-01.md).
- [Calibration and qualification failures](../strata-orca-iq3-xxs-calibration-2026-10-01.md).

Use [the maintained Strata guide](../../strata-backend.md) for release-pinned
registration, sidecar ownership and the current canonical IQ2_XS profiles.
