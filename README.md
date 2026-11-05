# ebs-gp3-audit

[![ci](https://github.com/moveeeax/ebs-gp3-audit/actions/workflows/ci.yml/badge.svg)](https://github.com/moveeeax/ebs-gp3-audit/actions/workflows/ci.yml)
[![Go](https://img.shields.io/badge/go-1.26%2B-00ADD8?logo=go)](https://go.dev/)
[![License: MIT](https://img.shields.io/badge/license-MIT-blue.svg)](LICENSE)

gp3 is cheaper and faster than gp2 at every size, yet most AWS accounts are full
of gp2 volumes nobody ever migrated. `ebs-gp3-audit` lists the volumes still on
legacy types and tells you exactly how much you would save per month by moving
each one to gp3 — as a table, JSON, or SARIF, with a `--fail-on` gate for CI.

It is strictly **read-only**: the only AWS call it makes is `ec2:DescribeVolumes`.

## How it works

1. Enumerate EBS volumes — live via `DescribeVolumes` (paginated) or from a JSON
   snapshot with `--from-file`.
2. For each `gp2`/`io1`/`io2` volume, compute the current monthly cost and the
   cost of an equivalent gp3 volume, preserving sustained IOPS. gp3 bills only
   IOPS above the free 3000 baseline and throughput above 125 MB/s, so the
   savings estimate stays honest for large volumes.
3. Report per-volume savings and a fleet total. `io1`/`io2` volumes whose
   provisioned IOPS exceed the gp3 maximum are called out as *not* gp3 targets.

Prices default to us-east-1 list rates and are fully overridable with `--prices`
for other regions or negotiated pricing.

## Install

Requires Go 1.26 or newer.

```shell
go install github.com/moveeeax/ebs-gp3-audit@latest
```

## Usage

```shell
# Audit every volume in the current account/region
ebs-gp3-audit audit

# JSON with a custom price file
ebs-gp3-audit audit --prices prices.json --output json > gp3.json

# Fail CI when unrealized monthly savings exceed $50, emit SARIF
ebs-gp3-audit audit --fail-on 50 --output sarif > gp3.sarif

# Offline: audit a snapshot instead of calling AWS
ebs-gp3-audit audit --from-file examples/volumes.json
```

Example table output:

```text
VOLUME        TYPE  SIZE(GiB)  NOW($/mo)  GP3($/mo)  SAVING($/mo)  STATUS
vol-0c7d8e9f  io1   200        285.00     21.00      264.00        MIGRATE
vol-0e4f5a6b  gp2   2000       200.00     175.00     25.00         MIGRATE
vol-0a1b2c3d  gp2   500        50.00      40.00      10.00         MIGRATE
vol-01a23b45  gp3   300        24.00      24.00      0.00          ok-gp3
vol-06f78c90  st1   4000       0.00       0.00       0.00          skip

Candidates: 3/5  Unrealized saving: $299.00/mo
```

## Flags

| Flag | Default | Description |
|------|---------|-------------|
| `--output`, `-o` | `table` | `table`, `json`, or `sarif` |
| `--region` | ambient | AWS region override |
| `--from-file` | — | read volumes from a JSON snapshot instead of AWS |
| `--prices` | built-in | JSON file overriding the price table |
| `--fail-on` | — | exit non-zero when unrealized monthly saving exceeds this many USD |

Exit code `2` means the `--fail-on` threshold was tripped — wire it into a
pipeline to keep unrealized savings from creeping back up.

## IAM

The tool needs a single read-only permission. Ship it with this policy
([`examples/iam-policy.json`](examples/iam-policy.json)):

```json
{
  "Version": "2012-10-17",
  "Statement": [
    { "Effect": "Allow", "Action": "ec2:DescribeVolumes", "Resource": "*" }
  ]
}
```

## Development

```shell
go test ./...      # unit + CLI tests
go vet ./...
go run . audit --from-file examples/volumes.json
```

## License

MIT — see [LICENSE](LICENSE).
