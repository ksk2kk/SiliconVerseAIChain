package distributed

import (
	"fmt"
	"math"
	"sync"
)

// Tensor represents a multi-dimensional array of float32 values.
// Tensors are the primitive data type for distributed computation.
type Tensor struct {
	Data   []float32
	Shape  []int // Dimensions
	Strides []int // Precomputed strides for indexing
}

// NewTensor creates a tensor with the given shape, zero-initialized.
func NewTensor(shape ...int) *Tensor {
	size := 1
	for _, d := range shape {
		size *= d
	}
	strides := computeStrides(shape)
	return &Tensor{
		Data:    make([]float32, size),
		Shape:   shape,
		Strides: strides,
	}
}

// NewTensorFromData creates a tensor with existing data.
func NewTensorFromData(data []float32, shape ...int) *Tensor {
	return &Tensor{
		Data:    data,
		Shape:   shape,
		Strides: computeStrides(shape),
	}
}

// Get returns the value at the given indices.
func (t *Tensor) Get(indices ...int) float32 {
	offset := t.offset(indices)
	return t.Data[offset]
}

// Set sets the value at the given indices.
func (t *Tensor) Set(value float32, indices ...int) {
	offset := t.offset(indices)
	t.Data[offset] = value
}

// offset computes the flat offset from multi-dimensional indices.
func (t *Tensor) offset(indices []int) int {
	if len(indices) != len(t.Shape) {
		panic(fmt.Sprintf("wrong number of indices: got %d, shape %v", len(indices), t.Shape))
	}
	offset := 0
	for i, idx := range indices {
		offset += idx * t.Strides[i]
	}
	return offset
}

// Size returns the total number of elements.
func (t *Tensor) Size() int {
	return len(t.Data)
}

// Reshape changes the tensor's shape without copying data.
func (t *Tensor) Reshape(shape ...int) *Tensor {
	newSize := 1
	for _, d := range shape {
		newSize *= d
	}
	if newSize != len(t.Data) {
		panic(fmt.Sprintf("reshape size mismatch: %d != %d", newSize, len(t.Data)))
	}
	t.Shape = shape
	t.Strides = computeStrides(shape)
	return t
}

// Slice extracts a sub-tensor along the first dimension.
// Like Python: tensor[start:end, ...]
func (t *Tensor) Slice(start, end int) *Tensor {
	if start < 0 || end > t.Shape[0] || start >= end {
		panic(fmt.Sprintf("invalid slice [%d:%d] for shape[0]=%d", start, end, t.Shape[0]))
	}
	newShape := make([]int, len(t.Shape))
	copy(newShape, t.Shape)
	newShape[0] = end - start

	offset := start * t.Strides[0]
	size := (end - start) * t.Strides[0]

	return &Tensor{
		Data:    t.Data[offset : offset+size],
		Shape:   newShape,
		Strides: computeStrides(newShape),
	}
}

// Add element-wise addition: t + other
func (t *Tensor) Add(other *Tensor) *Tensor {
	if len(t.Data) != len(other.Data) {
		panic("add: shape mismatch")
	}
	result := NewTensor(t.Shape...)
	for i := range result.Data {
		result.Data[i] = t.Data[i] + other.Data[i]
	}
	return result
}

// AddInPlace adds other into t in-place.
func (t *Tensor) AddInPlace(other *Tensor) {
	if len(t.Data) != len(other.Data) {
		panic("add_in_place: shape mismatch")
	}
	for i := range t.Data {
		t.Data[i] += other.Data[i]
	}
}

// Mul element-wise multiplication: t * other
func (t *Tensor) Mul(other *Tensor) *Tensor {
	if len(t.Data) != len(other.Data) {
		panic("mul: shape mismatch")
	}
	result := NewTensor(t.Shape...)
	for i := range result.Data {
		result.Data[i] = t.Data[i] * other.Data[i]
	}
	return result
}

// Scale multiplies all elements by a scalar.
func (t *Tensor) Scale(scalar float32) *Tensor {
	result := NewTensor(t.Shape...)
	for i := range result.Data {
		result.Data[i] = t.Data[i] * scalar
	}
	return result
}

// MatMul performs matrix multiplication: A[m,k] @ B[k,n] -> C[m,n]
func MatMul(a, b *Tensor) *Tensor {
	if len(a.Shape) != 2 || len(b.Shape) != 2 {
		panic("matmul: both tensors must be 2D")
	}
	m, k1 := a.Shape[0], a.Shape[1]
	k2, n := b.Shape[0], b.Shape[1]
	if k1 != k2 {
		panic(fmt.Sprintf("matmul: inner dims %d != %d", k1, k2))
	}

	result := NewTensor(m, n)
	for i := 0; i < m; i++ {
		rowOff := i * a.Strides[0]
		for j := 0; j < n; j++ {
			var sum float32
			for k := 0; k < k1; k++ {
				sum += a.Data[rowOff+k] * b.Data[k*b.Strides[0]+j]
			}
			result.Data[i*result.Strides[0]+j] = sum
		}
	}
	return result
}

// Gelu applies the GELU activation function element-wise.
func Gelu(x *Tensor) *Tensor {
	result := NewTensor(x.Shape...)
	for i, v := range x.Data {
		x := float64(v)
		result.Data[i] = 0.5 * v * (1.0 + float32(math.Tanh(0.79788456*(x+0.044715*x*x*x))))
	}
	return result
}

// SiLU applies SiLU (Swish) activation.
func SiLU(x *Tensor) *Tensor {
	result := NewTensor(x.Shape...)
	for i, v := range x.Data {
		if v >= 0 {
			result.Data[i] = v / (1.0 + float32(math.Exp(float64(-v))))
		} else {
			expv := float32(math.Exp(float64(v)))
			result.Data[i] = v * expv / (1.0 + expv)
		}
	}
	return result
}

// Softmax applies softmax along the last dimension.
func Softmax(x *Tensor) *Tensor {
	result := NewTensor(x.Shape...)
	lastDim := x.Shape[len(x.Shape)-1]

	for batch := 0; batch < x.Size()/lastDim; batch++ {
		offset := batch * lastDim
		// Find max for numerical stability
		maxVal := float32(math.Inf(-1))
		for j := 0; j < lastDim; j++ {
			if x.Data[offset+j] > maxVal {
				maxVal = x.Data[offset+j]
			}
		}
		// Exp and sum
		var sum float32
		for j := 0; j < lastDim; j++ {
			result.Data[offset+j] = float32(math.Exp(float64(x.Data[offset+j] - maxVal)))
			sum += result.Data[offset+j]
		}
		// Normalize
		for j := 0; j < lastDim; j++ {
			result.Data[offset+j] /= sum
		}
	}
	return result
}

// RMSNorm applies Root Mean Square normalization.
func RMSNorm(x *Tensor, weight *Tensor, epsilon float32) *Tensor {
	result := NewTensor(x.Shape...)
	lastDim := x.Shape[len(x.Shape)-1]

	for batch := 0; batch < x.Size()/lastDim; batch++ {
		offset := batch * lastDim
		var sumSq float32
		for j := 0; j < lastDim; j++ {
			sumSq += x.Data[offset+j] * x.Data[offset+j]
		}
		rms := float32(math.Sqrt(float64(sumSq/float32(lastDim) + epsilon)))
		for j := 0; j < lastDim; j++ {
			result.Data[offset+j] = x.Data[offset+j] / rms
			if weight != nil {
				result.Data[offset+j] *= weight.Data[j]
			}
		}
	}
	return result
}

// computeStrides precomputes strides for efficient indexing.
func computeStrides(shape []int) []int {
	strides := make([]int, len(shape))
	if len(strides) > 0 {
		strides[len(strides)-1] = 1
		for i := len(strides) - 2; i >= 0; i-- {
			strides[i] = strides[i+1] * shape[i+1]
		}
	}
	return strides
}

// Parallel matmul utilities for distributed computing.
var _ = sync.Mutex{}
var _ = fmt.Sprintf
