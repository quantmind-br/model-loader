# Security Policy

## Supported versions

model-loader is under active development and has not reached a stable v1
release. Security fixes are applied to the latest code on the default branch.
Older commits and locally modified builds are not supported separately.

## Report a vulnerability

Do not open a public issue for a suspected vulnerability.

Use GitHub's **Security** tab and select **Report a vulnerability** to submit a
private advisory to the maintainers. Include:

- the affected commit or version;
- the environment and backend involved;
- reproduction steps or a minimal proof of concept;
- the expected and observed impact; and
- any mitigation you have already tested.

Do not include real credentials, private model data, or unrelated personal
information. The maintainers will acknowledge the report through the private
advisory and coordinate disclosure after a fix is available.

## Deployment boundary

model-loader is built for a single trusted operator. Its HTTP proxy:

- binds to `127.0.0.1` by default;
- does not authenticate clients;
- launches local executables configured by the operator; and
- can load models and terminate managed process groups.

Binding it to a non-loopback address changes the threat model. Place an
authenticated, encrypted reverse proxy in front of it, restrict network access,
and treat profile, backend, and state directories as sensitive operator data.
