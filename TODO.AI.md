# AI Implementation TODO

- [ ] Implement and verify PART 11 secret rotation, including `installation_secret` grace handling and sensitive-operation authorization.
- [ ] Align PART 10/11 `app_secrets` storage with the versioned schema and removed-node revocation history.
- [ ] Emit the required warning when backup size retention preserves an incremental base above the configured cap.
- [ ] Rerun `tests/incus.sh openrc` when Incus guest network egress is available; the guest could not reliably reach package mirrors during this run.
- [ ] Confirm the pushed commit's CI, Devel Build, and Docker Build workflows complete successfully.
