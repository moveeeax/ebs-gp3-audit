# ebs-gp3-audit

[![ci](https://github.com/moveeeax/ebs-gp3-audit/actions/workflows/ci.yml/badge.svg)](https://github.com/moveeeax/ebs-gp3-audit/actions/workflows/ci.yml)
[![Go](https://img.shields.io/badge/go-1.26%2B-00ADD8?logo=go)](https://go.dev/)
[![License: MIT](https://img.shields.io/badge/license-MIT-blue.svg)](LICENSE)

gp3 is cheaper and faster than gp2 at every size, yet most AWS accounts are full
of gp2 volumes nobody ever migrated. `ebs-gp3-audit` lists the volumes still on
legacy types and tells you exactly how much you would save per month by moving
each one to gp3 — as a table, JSON, or SARIF, with a `--fail-on` gate for CI,
and can write the `modify-volume` commands for you.

It is strictly **read-only**: the only AWS call it makes is `ec2:DescribeVolumes`.
The optional migration plan is a file you review and run yourself.

## How it works

1. Enumerate EBS volumes — live via `DescribeVolumes` (paginated) or from a JSON
   snapshot with `--from-file`.
2. For each `gp2`/`io1`/`io2` volume, compute the current monthly cost and the
   cost of an equivalent gp3 volume, preserving sustained IOPS. gp3 bills only
   IOPS above the free 3000 baseline and throughput above 125 MiB/s, so the
   savings estimate stays honest for large volumes.
   - gp2 throughput is taken from the AWS gp2 table (128 MiB/s up to 170 GiB,
     250 MiB/s above) and the gp3 target is provisioned to match, so the
     migration does not lower throughput.
   - `DescribeVolumes` returns no throughput for io1/io2, so it is derived
     from provisioned IOPS per the
     [Provisioned IOPS SSD docs](https://docs.aws.amazon.com/ebs/latest/userguide/provisioned-iops.html):
     io1 delivers 0.25 MiB/s per IOPS up to 500 MiB/s (reached at 2,000 IOPS),
     then 16 KiB per IOPS above 32,000 IOPS, up to 1,000 MiB/s at 64,000 IOPS;
     io2 (Block Express) delivers 0.256 MiB/s per IOPS, up to 4,000 MiB/s.
     The gp3 target keeps that throughput, adding IOPS for the 0.25 MiB/s per
     IOPS rule. An io2 volume above 2,000 MiB/s (more than 7,812 IOPS) has no
     gp3 equivalent and is not a candidate; the reason says why.
   - `--match-performance baseline` prices gp2/io1/io2 against gp3 at
     125 MiB/s instead, and the reason notes the throughput the volume would
     lose.
   - The gp3 target follows the gp3 limits: up to 64 TiB, 80,000 IOPS and
     2,000 MiB/s, at most 500 IOPS per GiB and 0.25 MiB/s per provisioned
     IOPS. When a volume needs more IOPS than its size allows, the target is
     sized up (and priced) accordingly, e.g. 80,000 IOPS needs 160 GiB, and the
     reason says so.
3. Report per-volume savings, the gp3 size/IOPS/throughput that was priced,
   and a fleet total. Volumes whose IOPS, throughput or size exceed the gp3
   maximums are called out as *not* gp3 targets.
4. Optionally write a migration plan (`--emit`): an `aws ec2 modify-volume`
   script or Terraform attribute suggestions with the same values.

Prices default to us-east-1 list rates and are fully overridable with `--prices`
for other regions or negotiated pricing. The gp3 limits live in the same file
(see [Price file](#price-file)), so a region or an Outpost still on the
pre-September-2025 limits can pin them.

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

# Write a dry-run-by-default migration script, or Terraform suggestions
ebs-gp3-audit audit --emit plan.sh
ebs-gp3-audit audit --emit gp3.tf --emit-format terraform
```

Example table output:

```text
VOLUME        TYPE  SIZE(GiB)  NOW($/mo)  GP3($/mo)  SAVING($/mo)  GP3 SIZE(GiB)  GP3 IOPS  GP3 TPUT(MiB/s)  STATUS
vol-0c7d8e9f  io1   200        285.00     36.00      249.00        200            4000      500              MIGRATE
vol-0e4f5a6b  gp2   2000       200.00     180.00     20.00         2000           6000      250              MIGRATE
vol-0a1b2c3d  gp2   500        50.00      45.00      5.00          500            3000      250              MIGRATE
vol-01a23b45  gp3   300        24.00      24.00      0.00          300            3000      125              ok-gp3
vol-06f78c90  st1   4000       0.00       0.00       0.00          -              -         -                skip

Candidates: 3/5  Unrealized saving: $274.00/mo
```

JSON findings carry the same target as `gp3_size_gib`, `gp3_iops` and
`gp3_throughput`.

## Flags

| Flag | Default | Description |
|------|---------|-------------|
| `--output`, `-o` | `table` | `table`, `json`, or `sarif` |
| `--region` | ambient | AWS region override |
| `--from-file` | — | read volumes from a JSON snapshot instead of AWS |
| `--prices` | built-in | JSON file overriding the price table |
| `--fail-on` | — | exit non-zero when unrealized monthly saving exceeds this many USD |
| `--match-performance` | `gp2` | gp3 target throughput for gp2/io1/io2: `gp2` keeps the volume's current throughput, `baseline` prices at 125 MiB/s |
| `--emit` | — | write a migration plan for the candidates to this file |
| `--emit-format` | `cli` | plan format: `cli` (`aws ec2 modify-volume` script) or `terraform` |

Exit code `2` means the `--fail-on` threshold was tripped — wire it into a
pipeline to keep unrealized savings from creeping back up.

## Price file

`--prices` takes a JSON object; any field it leaves out keeps the built-in
value ([`examples/prices.json`](examples/prices.json) overrides prices only).

| Field | Default | Meaning |
|-------|---------|---------|
| `gp2_gb`, `gp3_gb`, `io1_gb`, `io2_gb` | 0.10, 0.08, 0.125, 0.125 | $/GiB-month |
| `gp3_iops`, `io1_iops`, `io2_iops` | 0.005, 0.065, 0.065 | $/IOPS-month (gp3: above baseline) |
| `gp3_throughput` | 0.04 | $/MiB/s-month above baseline |
| `gp3_baseline_iops`, `gp3_baseline_throughput` | 3000, 125 | free gp3 performance |
| `gp3_max_iops`, `gp3_max_throughput`, `gp3_max_size_gib` | 80000, 2000, 65536 | gp3 maximums |
| `gp3_iops_per_gib` | 500 | max provisioned IOPS per GiB |
| `gp3_throughput_per_iops` | 0.25 | max MiB/s per provisioned IOPS |

For gp3 on Outposts (16 TiB, 16,000 IOPS, 1,000 MiB/s):

```json
{ "gp3_max_iops": 16000, "gp3_max_throughput": 1000, "gp3_max_size_gib": 16384 }
```

## Migration plan

`--emit plan.sh` writes one `aws ec2 modify-volume --volume-id … --volume-type
gp3 --iops N --throughput M` call per candidate (plus `--size` when gp3 needs a
larger volume for the IOPS, and `--region` when the volume came from AWS). The
script runs every call with `--dry-run` unless `APPLY=1` is set:

```shell
./plan.sh            # dry run: AWS checks permissions and parameters only
APPLY=1 ./plan.sh    # apply
```

EBS allows up to four modifications per volume in a rolling 24-hour period,
each one has to reach `completed` before the next, and a modification cannot be
cancelled ([Elastic Volumes considerations](https://docs.aws.amazon.com/ebs/latest/userguide/ebs-modify-volume.html)).
Running the script needs `ec2:ModifyVolume`; `ebs-gp3-audit` itself does not.

`--emit gp3.tf --emit-format terraform` writes a `locals` map keyed by volume ID
with `volume_type`, `volume_size`, `iops` and `throughput` per candidate (the
names used by `aws_instance` block devices; `aws_ebs_volume` calls the first two
`type` and `size`).

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
