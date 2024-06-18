package cloudevents

import (
	"github.com/spf13/cobra"
)

// CloudEventsOptions defines the flags for addon manager
type CloudEventsOptions struct {
	WorkDriver          string
	WorkDriverConfig    string
	CloudEventsClientID string
	SourceID            string
}

// NewCloudEventsOptions returns the flags with default value set
func NewCloudEventsOptions() *CloudEventsOptions {
	return &CloudEventsOptions{
		// set default work driver to kube
		WorkDriver: "kube",
	}
}

// AddFlags adds the cloudevents options flags to the given command
func (o *CloudEventsOptions) AddFlags(cmd *cobra.Command) {
	flags := cmd.Flags()
	flags.StringVar(&o.WorkDriver, "work-driver",
		o.WorkDriver, "The type of work driver, currently it can be kube, mqtt or grpc")
	flags.StringVar(&o.WorkDriverConfig, "work-driver-config",
		o.WorkDriverConfig, "The config file path of current work driver")
	flags.StringVar(&o.CloudEventsClientID, "cloudevents-client-id",
		o.CloudEventsClientID, "The ID of the cloudevents client when publishing works with cloudevents")
	flags.StringVar(&o.SourceID, "source-id",
		o.SourceID, "The ID of the source when publishing works with cloudevents")
}
