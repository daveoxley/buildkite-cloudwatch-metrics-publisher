package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"time"

	"github.com/apex/go-apex"
	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/cloudwatch"
	"github.com/buildkite/buildkite-cloudwatch-metrics-publisher/functions/collect-metrics/buildkite"
)

// Generates:
// Buildkite > RunningBuildsCount
// Buildkite > RunningJobsCount
// Buildkite > ScheduledBuildsCount
// Buildkite > ScheduledJobsCount
// Buildkite > (Queue) > RunningBuildsCount
// Buildkite > (Queue) > RunningJobsCount
// Buildkite > (Queue) > ScheduledBuildsCount
// Buildkite > (Queue) > ScheduledJobsCount

func main() {
	apex.HandleFunc(func(event json.RawMessage, ctx *apex.Context) (interface{}, error) {
		svc := cloudwatch.New(session.New())

		var conf Config
		if err := json.Unmarshal(event, &conf); err != nil {
			return nil, err
		}

		if conf.BuildkiteApiAccessToken == "" {
			return nil, errors.New("No BuildkiteApiAccessToken provided")
		}

		if conf.BuildkiteOrgSlug == "" {
			return nil, errors.New("No BuildkiteOrgSlug provided")
		}

		if conf.Queue == "" {
			return nil, errors.New("No Queue provided")
		}

		var res = &Result{
			Queues:    map[string]Counts{},
		}

		if err := res.getBuildStats(conf, "running"); err != nil {
			return nil, err
		}

		if err := res.getBuildStats(conf, "scheduled"); err != nil {
			return nil, err
		}

		log.Printf("Extracting cloudwatch metrics from results")
		metrics := res.extractMetricData()

		for _, chunk := range chunkMetricData(10, metrics) {
			log.Printf("Submitting chunk of %d metrics to Cloudwatch", len(chunk))
			if err := putMetricData(svc, chunk); err != nil {
				return nil, err
			}
		}

		return res, nil
	})
}

type Config struct {
	BuildkiteOrgSlug, BuildkiteApiAccessToken, Queue string
}

type Counts struct {
	RunningBuilds, RunningJobs, ScheduledBuilds, ScheduledJobs int
}

func (c Counts) addBuild(build buildkite.Build) Counts {
	switch build.State {
	case "running":
		c.RunningBuilds++
	case "scheduled":
		c.ScheduledBuilds++
	}
	return c
}

func (c Counts) addJob(job buildkite.Job) Counts {
	switch job.State {
	case "running":
		c.RunningJobs++
	case "scheduled":
		c.ScheduledJobs++
	}
	return c
}

func (c Counts) asMetrics(dimensions []*cloudwatch.Dimension) []*cloudwatch.MetricDatum {
	return []*cloudwatch.MetricDatum{
		&cloudwatch.MetricDatum{
			MetricName: aws.String("RunningBuildsCount"),
			Dimensions: dimensions,
			Value:      aws.Float64(float64(c.RunningBuilds)),
			Unit:       aws.String("Count"),
		},
		&cloudwatch.MetricDatum{
			MetricName: aws.String("ScheduledBuildsCount"),
			Dimensions: dimensions,
			Value:      aws.Float64(float64(c.ScheduledBuilds)),
			Unit:       aws.String("Count"),
		},
		&cloudwatch.MetricDatum{
			MetricName: aws.String("RunningJobsCount"),
			Dimensions: dimensions,
			Value:      aws.Float64(float64(c.RunningJobs)),
			Unit:       aws.String("Count"),
		},
		&cloudwatch.MetricDatum{
			MetricName: aws.String("ScheduledJobsCount"),
			Dimensions: dimensions,
			Value:      aws.Float64(float64(c.ScheduledJobs)),
			Unit:       aws.String("Count"),
		},
	}
}

type Result struct {
	Counts
	Queues map[string]Counts
}

func (r Result) extractMetricData() []*cloudwatch.MetricDatum {
	data := []*cloudwatch.MetricDatum{}
	data = append(data, r.Counts.asMetrics(nil)...)

	for name, c := range r.Queues {
		data = append(data, c.asMetrics([]*cloudwatch.Dimension{
			{Name: aws.String("Queue"), Value: aws.String(name)},
		})...)
	}

	return data
}

func (res *Result) getBuildStats(conf Config, stateFilter string) (error) {
	log.Printf("Querying buildkite for %s builds for org %s", stateFilter, conf.BuildkiteOrgSlug)
	builds, err := buildkiteBuilds(conf.BuildkiteOrgSlug, conf.BuildkiteApiAccessToken, stateFilter)
	if err != nil {
		return err
	}

	log.Printf("Aggregating results from %d builds", len(builds))
	for _, build := range builds {
		res.Counts = res.Counts.addBuild(build)

		var buildQueues = map[string]int{}
		for _, job := range build.Jobs {
			res.Counts = res.Counts.addJob(job)
			queue := job.Queue()
			if queue == "default" || queue == conf.Queue {
				res.Queues[queue] = res.Queues[queue].addJob(job)
				buildQueues[queue]++
			}
		}

		for queue := range buildQueues {
			res.Queues[queue] = res.Queues[queue].addBuild(build)
		}
	}

	// Initialise both queues if they don't have any jobs
	res.Queues["default"] = res.Queues["default"]
	res.Queues[conf.Queue] = res.Queues[conf.Queue]

	log.Printf("%+v\n", *res)

	return nil
}

func buildkiteBuilds(orgSlug, apiKey, stateFilter string) ([]buildkite.Build, error) {
	url := fmt.Sprintf(
		"https://api.buildkite.com/v2/organizations/%s/builds?state=%s&page=%d&per_page=100",
		orgSlug,
		stateFilter,
		1,
	)

	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, err
	}

	req.Header.Add("Authorization", fmt.Sprintf("Bearer %s", apiKey))

	//Issue the request and get the bearer token from the JSON you get back
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}

	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("Failed to request %s", url)
	}

	// TODO: Pagination, but ain't nobody got time for that.
	// log.Printf("%#v", resp.Header.Get("Link"))

	var builds []buildkite.Build
	if err = json.NewDecoder(resp.Body).Decode(&builds); err != nil {
		return nil, err
	}

	return builds, nil
}

func chunkMetricData(size int, data []*cloudwatch.MetricDatum) [][]*cloudwatch.MetricDatum {
	var chunks = [][]*cloudwatch.MetricDatum{}
	for i := 0; i < len(data); i += size {
		end := i + size
		if end > len(data) {
			end = len(data)
		}
		chunks = append(chunks, data[i:end])
	}
	return chunks
}

func putMetricData(svc *cloudwatch.CloudWatch, data []*cloudwatch.MetricDatum) error {
	_, err := svc.PutMetricData(&cloudwatch.PutMetricDataInput{
		MetricData: data,
		Namespace:  aws.String("Buildkite"),
	})
	if err != nil {
		return err
	}

	return nil
}
