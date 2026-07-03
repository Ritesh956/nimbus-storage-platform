package storage

import (
	"context"
	"fmt"
	"net/url"
	"time"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
)

// bucketName holds every chunk, content-addressed by SHA-256 hash as the
// object key (docs/02-system-design.md §2.1).
const bucketName = "nimbus-chunks"

// region is pinned to MinIO's conventional default rather than left blank.
// minio-go otherwise resolves the bucket's region with a live
// GetBucketLocation call the first time it's needed — which, for the
// "public" client (endpoint = localhost, meant only for external callers),
// would have nimbus-api itself trying to dial localhost from inside its
// own container and failing. Pinning it skips that lookup entirely.
const region = "us-east-1"

// presignExpiry bounds how long an issued PUT/GET URL stays valid.
const presignExpiry = 15 * time.Minute

func buildMinIOClient(endpoint, accessKey, secretKey string) (*minio.Client, error) {
	u, err := url.Parse(endpoint)
	if err != nil {
		return nil, fmt.Errorf("parse endpoint %q: %w", endpoint, err)
	}
	return minio.New(u.Host, &minio.Options{
		Creds:  credentials.NewStaticV4(accessKey, secretKey, ""),
		Secure: u.Scheme == "https",
		Region: region,
	})
}

// EnsureBuckets creates bucketName on every node if it doesn't already
// exist. Uses the internal (docker-network) client since this call
// originates from inside nimbus-api's own container.
func (rt *Router) EnsureBuckets(ctx context.Context) error {
	rt.mu.RLock()
	nodes := make([]StorageNode, 0, len(rt.nodes))
	for _, n := range rt.nodes {
		nodes = append(nodes, n)
	}
	rt.mu.RUnlock()

	for _, n := range nodes {
		cli := rt.internalMinio[n.ID]
		if cli == nil {
			return fmt.Errorf("no internal MinIO client for node %s", n.ID)
		}
		exists, err := cli.BucketExists(ctx, bucketName)
		if err != nil {
			return fmt.Errorf("check bucket on node %s: %w", n.ID, err)
		}
		if !exists {
			if err := cli.MakeBucket(ctx, bucketName, minio.MakeBucketOptions{Region: region}); err != nil {
				return fmt.Errorf("create bucket on node %s: %w", n.ID, err)
			}
		}
	}
	return nil
}

// PresignPut returns a client-usable PUT URL for objectKey on node, signed
// against the node's PublicEndpoint — S3 SigV4 signs the Host header, so
// signing against the internal docker-network hostname would produce a URL
// no external client could ever satisfy (see StorageNode.PublicEndpoint).
func (rt *Router) PresignPut(ctx context.Context, node NodeID, objectKey string) (string, time.Time, error) {
	rt.mu.RLock()
	cli := rt.publicMinio[node]
	rt.mu.RUnlock()
	if cli == nil {
		return "", time.Time{}, fmt.Errorf("no public MinIO client for node %s", node)
	}
	u, err := cli.PresignedPutObject(ctx, bucketName, objectKey, presignExpiry)
	if err != nil {
		return "", time.Time{}, err
	}
	return u.String(), time.Now().Add(presignExpiry), nil
}

// PresignGet is the read-path counterpart of PresignPut.
func (rt *Router) PresignGet(ctx context.Context, node NodeID, objectKey string) (string, time.Time, error) {
	rt.mu.RLock()
	cli := rt.publicMinio[node]
	rt.mu.RUnlock()
	if cli == nil {
		return "", time.Time{}, fmt.Errorf("no public MinIO client for node %s", node)
	}
	u, err := cli.PresignedGetObject(ctx, bucketName, objectKey, presignExpiry, url.Values{})
	if err != nil {
		return "", time.Time{}, err
	}
	return u.String(), time.Now().Add(presignExpiry), nil
}
