package sop

import (
	"errors"
	"math"
)

type Vector3 [3]float64

type CanonicalPose struct {
	CameraPosition Vector3
	ImageUp        Vector3
}

var (
	ErrZeroVector      = errors.New("direction vector cannot be zero")
	ErrNonFiniteVector = errors.New("direction vector must contain finite numbers")
	ErrParallelVectors = errors.New("camera and image-up directions cannot be parallel")
)

const parallelThreshold = 0.999

func CanonicalizePose(cameraPosition, imageUp Vector3) (CanonicalPose, error) {
	p, err := normalize(cameraPosition)
	if err != nil {
		return CanonicalPose{}, err
	}
	u, err := normalize(imageUp)
	if err != nil {
		return CanonicalPose{}, err
	}
	if math.Abs(dot(p, u)) >= parallelThreshold {
		return CanonicalPose{}, ErrParallelVectors
	}
	projection := dot(u, p)
	u, err = normalize(Vector3{u[0] - projection*p[0], u[1] - projection*p[1], u[2] - projection*p[2]})
	if err != nil {
		return CanonicalPose{}, err
	}
	return CanonicalPose{CameraPosition: round6(p), ImageUp: round6(u)}, nil
}

func normalize(v Vector3) (Vector3, error) {
	for _, n := range v {
		if math.IsNaN(n) || math.IsInf(n, 0) {
			return Vector3{}, ErrNonFiniteVector
		}
	}
	length := math.Sqrt(dot(v, v))
	if length < 1e-9 {
		return Vector3{}, ErrZeroVector
	}
	return Vector3{v[0] / length, v[1] / length, v[2] / length}, nil
}

func dot(a, b Vector3) float64 { return a[0]*b[0] + a[1]*b[1] + a[2]*b[2] }

func round6(v Vector3) Vector3 {
	return Vector3{math.Round(v[0]*1e6) / 1e6, math.Round(v[1]*1e6) / 1e6, math.Round(v[2]*1e6) / 1e6}
}
