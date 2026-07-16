package sop

import (
	"errors"
	"math"
	"testing"
)

func TestCanonicalizePoseNormalizesAndOrthogonalizes(t *testing.T) {
	got, err := CanonicalizePose(Vector3{0, -1, 1}, Vector3{1, 0.02, 0})
	if err != nil {
		t.Fatal(err)
	}
	wantCamera := Vector3{0, -0.707107, 0.707107}
	for i := range wantCamera {
		if math.Abs(got.CameraPosition[i]-wantCamera[i]) > 0.000001 {
			t.Fatalf("camera[%d] = %f, want %f", i, got.CameraPosition[i], wantCamera[i])
		}
	}
	if math.Abs(dot(got.CameraPosition, got.ImageUp)) > 0.000001 {
		t.Fatalf("canonical vectors are not orthogonal: %#v", got)
	}
}

func TestCanonicalizePoseRejectsInvalidVectors(t *testing.T) {
	cases := []struct {
		name       string
		camera, up Vector3
		want       error
	}{
		{"zero", Vector3{}, Vector3{1, 0, 0}, ErrZeroVector},
		{"non-finite", Vector3{0, 0, math.Inf(1)}, Vector3{1, 0, 0}, ErrNonFiniteVector},
		{"parallel", Vector3{0, 0, 1}, Vector3{0, 0, 2}, ErrParallelVectors},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := CanonicalizePose(tc.camera, tc.up)
			if !errors.Is(err, tc.want) {
				t.Fatalf("error = %v, want %v", err, tc.want)
			}
		})
	}
}
