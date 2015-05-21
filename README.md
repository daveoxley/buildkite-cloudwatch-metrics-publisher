# Buildkite Cloudwatch Metrics Publisher

Metrics publisher is a small script for publishing your [Buildkite](https://buildkite.com/) job queue statistics to AWS Cloud Watch for auto-scaling your [Buildkite Agents](https://github.com/buildkite/agent).

![Screenshot of AWS metrics](http://i.imgur.com/jqVdlA2.png)

Available tags:

* `latest` ([source](https://github.com/buildkite/buildkite-cloudwatch-metrics-publisher/blob/master/Dockerfile)) - Alpine Linux based

## Published CloudWatch Metrics

The following AWS CloudWatch metrics will be published:

* Buildkite > RunningBuilds
* Buildkite > RunningJobs
* Buildkite > ScheduledBuilds
* Buildkite > ScheduledJobs

Each metric is also reported with an additional Project dimension, so you can monitor your build queues on both a global and per-project basis.

## Prerequisites

1. A [Buildkite API Access Token](https://buildkite.com/user/api-access-tokens) with `read_projects` permission.

2. A [AWS IAM Policy](https://console.aws.amazon.com/iam/home) (e.g. `buildkite-cloudwatch-metrics-publisher`) with the following policy document:

```
{
    "Version": "2012-10-17",
    "Statement": [
        {
            "Sid": "Stmt1432216114000",
            "Effect": "Allow",
            "Action": [
                "cloudwatch:PutMetricData"
            ],
            "Resource": [
                "*"
            ]
        }
    ]
}
```

2. A [AWS IAM User](https://console.aws.amazon.com/iam/home) (e.g. `buildkite-cloudwatch-metrics-publisher`) with the above policy attached.

## Setup

Create a `buildkite-cloudwatch-metrics-publisher.env` file like below, copying in the credentials of the AWS user, the Buildkite API access token, your AWS region and your Buildkite organization's slug:

```
BUILDKITE_ORG_SLUG=my-org
BUILDKITE_API_ACCESS_TOKEN=xxx
AWS_ACCESS_KEY_ID=yyy
AWS_SECRET_ACCESS_KEY=zzz
AWS_DEFAULT_REGION=us-east-1
```

## Running

The simplest way is using Docker:

```
docker run -d \
  --name buildkite-cloudwatch-metrics-publisher \
  --env-file=buildkite-cloudwatch-metrics-publisher.env \
  buildkite/cloudwatch-metrics-publisher
```

To tail the logs:

```
docker logs -f buildkite-cloudwatch-metrics-publisher
```

### Without Docker

You'll need [jq](http://stedolan.github.io/jq/) and [aws-cli](http://aws.amazon.com/cli/) installed.

```
source buildkite-cloudwatch-metrics-publisher.env && \
./buildkite-cloudwatch-metrics-publisher
```

## Development

```
docker build -t bk-cw-metrics-publisher .
docker run -it \
  --rm=true \
  --env-file=buildkite-cloudwatch-metrics-publisher.env \
  bk-cw-metrics-publisher
```

## License

See [LICENSE.md](LICENSE.md) (MIT)