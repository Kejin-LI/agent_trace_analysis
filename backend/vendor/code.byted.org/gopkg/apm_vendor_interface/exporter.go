package apm_vendor_interface

import "io"

// MetricsExporter is an interface for exporting metrics data.
// It includes methods for writing and closing, as well as getting tenant and batch size information.
type MetricsExporter interface {
	// WriteCloser is an interface that groups the basic Write and Close methods.
	io.WriteCloser

	// GetTenant returns the tenant information to which this exporter is bound.
	// This can be used to manage data isolation between different tenants.
	GetTenant() string

	// GetBatchSize returns the recommended message batch size for this exporter.
	// This can be used to optimize the data transmission process.
	GetBatchSize() int
}
