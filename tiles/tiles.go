// Package tiles holds small composable types for future raster and coastline-mask renderers.
// No coastline masking or raster reprojection is implemented in v0.1.0.
package tiles

type Attribution struct{ Source, Dataset, ObservationTime, Resolution string }
