// Package processing is nimbus-worker's domain logic: consume
// upload.completed events, reassemble the original chunks, generate a
// thumbnail, and record the result — see docs/02-system-design.md §6 and
// docs/09-roadmap.md Day 9.
package processing

// maxThumbnailDimension bounds the longer edge of a generated thumbnail;
// images are never upscaled past their original size.
const maxThumbnailDimension = 256
