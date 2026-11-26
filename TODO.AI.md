# AI Implementation TODO

- [ ] Implement and verify PART 19's per-source blocklist timestamp contract and optional allowlist-description behavior.
- [ ] Implement and verify PART 22's staged auto-update lifecycle: privilege-aware staging, trusted checksum re-fetch before root replacement, failure notification/audit with same-version retry suppression, supervisor restart behavior, and documentation.
- [ ] Rerun `tests/incus.sh openrc` when Incus guest network egress is available; the guest could not reliably reach package mirrors during this run.
- [ ] Confirm the pushed commit's CI, Devel Build, and Docker Build workflows complete successfully.
