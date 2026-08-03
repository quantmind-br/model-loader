# Known issues

New defects are tracked in [GitHub Issues](https://github.com/quantmind-br/model-loader/issues).
Search existing issues before filing a report and use the bug-report template so
the backend, environment, reproduction steps, and relevant logs are captured.

## Open investigation

### Managed backend exits before health

Some early local launches exited before their health check completed even though
the same backend command later ran successfully. The original logs and profile no
longer exist, and the generic backend shutdown message was not diagnostic. A new
report needs a reproducible launch and its complete backend log before this can be
investigated further.

## Historical identifiers

Older regression-test comments contain identifiers such as `P4`, `BR1`, and
`AUD-A1`. They refer to resolved defects recorded in the Git history before the
`v0.1.0` public release. The identifiers remain in tests because they document
the reason each regression boundary exists; they are not open issue numbers.
