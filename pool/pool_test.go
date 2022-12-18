package pool

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestNewPool(t *testing.T) {
	pool := NewPool(EmptyPoolCapacity)
	defer pool.Clear()
	assert.NotNil(t, pool)
	assert.NotNil(t, pool.Pool())
	assert.Equal(t, 0, pool.Size())
}

func TestPool_Put(t *testing.T) {
	pool := NewPool(EmptyPoolCapacity)
	defer pool.Clear()
	assert.NotNil(t, pool)
	assert.NotNil(t, pool.Pool())
	assert.Equal(t, 0, pool.Size())
	pool.Put("client1.ID", "client1")
	assert.Equal(t, 1, pool.Size())
	pool.Put("client2.ID", "client2")
	assert.Equal(t, 2, pool.Size())
}

//nolint:dupl
func TestPool_Pop(t *testing.T) {
	pool := NewPool(EmptyPoolCapacity)
	defer pool.Clear()
	assert.NotNil(t, pool)
	assert.NotNil(t, pool.Pool())
	assert.Equal(t, 0, pool.Size())
	pool.Put("client1.ID", "client1")
	assert.Equal(t, 1, pool.Size())
	pool.Put("client2.ID", "client2")
	assert.Equal(t, 2, pool.Size())
	if c1, ok := pool.Pop("client1.ID").(string); !ok {
		assert.Equal(t, c1, "client1")
	} else {
		assert.Equal(t, "client1", c1)
		assert.Equal(t, 1, pool.Size())
	}
	if c2, ok := pool.Pop("client2.ID").(string); !ok {
		assert.Equal(t, c2, "client2")
	} else {
		assert.Equal(t, "client2", c2)
		assert.Equal(t, 0, pool.Size())
	}
}

func TestPool_Clear(t *testing.T) {
	pool := NewPool(EmptyPoolCapacity)
	defer pool.Clear()
	assert.NotNil(t, pool)
	assert.NotNil(t, pool.Pool())
	assert.Equal(t, 0, pool.Size())
	pool.Put("client1.ID", "client1")
	assert.Equal(t, 1, pool.Size())
	pool.Put("client2.ID", "client2")
	assert.Equal(t, 2, pool.Size())
	pool.Clear()
	assert.Equal(t, 0, pool.Size())
}

func TestPool_ForEach(t *testing.T) {
	pool := NewPool(EmptyPoolCapacity)
	defer pool.Clear()
	assert.NotNil(t, pool)
	assert.NotNil(t, pool.Pool())
	assert.Equal(t, 0, pool.Size())
	pool.Put("client1.ID", "client1")
	assert.Equal(t, 1, pool.Size())
	pool.Put("client2.ID", "client2")
	assert.Equal(t, 2, pool.Size())
	pool.ForEach(func(key, value interface{}) bool {
		if c, ok := value.(string); ok {
			assert.NotEmpty(t, c)
		}
		return true
	})
}

//nolint:dupl
func TestPool_Get(t *testing.T) {
	pool := NewPool(EmptyPoolCapacity)
	defer pool.Clear()
	assert.NotNil(t, pool)
	assert.NotNil(t, pool.Pool())
	assert.Equal(t, 0, pool.Size())
	pool.Put("client1.ID", "client1")
	assert.Equal(t, 1, pool.Size())
	pool.Put("client2.ID", "client2")
	assert.Equal(t, 2, pool.Size())
	if c1, ok := pool.Get("client1.ID").(string); !ok {
		assert.Equal(t, c1, "client1")
	} else {
		assert.Equal(t, "client1", c1)
		assert.Equal(t, 2, pool.Size())
	}
	if c2, ok := pool.Get("client2.ID").(string); !ok {
		assert.Equal(t, c2, "client2")
	} else {
		assert.Equal(t, "client2", c2)
		assert.Equal(t, 2, pool.Size())
	}
}

func TestPool_GetOrPut(t *testing.T) {
	pool := NewPool(EmptyPoolCapacity)
	defer pool.Clear()
	assert.NotNil(t, pool)
	assert.NotNil(t, pool.Pool())
	assert.Equal(t, 0, pool.Size())
	pool.Put("client1.ID", "client1")
	assert.Equal(t, 1, pool.Size())
	pool.Put("client2.ID", "client2")
	assert.Equal(t, 2, pool.Size())
	c1, loaded, err := pool.GetOrPut("client1.ID", "client1")
	assert.True(t, loaded)
	if c1, ok := c1.(string); !ok {
		assert.Equal(t, c1, "client1")
	} else {
		assert.Equal(t, "client1", c1)
		assert.Equal(t, 2, pool.Size())
	}
	assert.Nil(t, err)
	c2, loaded, err := pool.GetOrPut("client2.ID", "client2")
	assert.True(t, loaded)
	if c2, ok := c2.(string); !ok {
		assert.Equal(t, c2, "client2")
	} else {
		assert.Equal(t, "client2", c2)
		assert.Equal(t, 2, pool.Size())
	}
	assert.Nil(t, err)
}

func TestPool_Remove(t *testing.T) {
	pool := NewPool(EmptyPoolCapacity)
	defer pool.Clear()
	assert.NotNil(t, pool)
	assert.NotNil(t, pool.Pool())
	assert.Equal(t, 0, pool.Size())
	pool.Put("client1.ID", "client1")
	assert.Equal(t, 1, pool.Size())
	pool.Put("client2.ID", "client2")
	assert.Equal(t, 2, pool.Size())
	pool.Remove("client1.ID")
	assert.Equal(t, 1, pool.Size())
	pool.Remove("client2.ID")
	assert.Equal(t, 0, pool.Size())
}

func TestPool_GetClientIDs(t *testing.T) {
	pool := NewPool(EmptyPoolCapacity)
	defer pool.Clear()
	assert.NotNil(t, pool)
	assert.NotNil(t, pool.Pool())
	assert.Equal(t, 0, pool.Size())
	pool.Put("client1.ID", "client1")
	assert.Equal(t, 1, pool.Size())
	pool.Put("client2.ID", "client2")
	assert.Equal(t, 2, pool.Size())

	var ids []string
	pool.ForEach(func(key, value interface{}) bool {
		if id, ok := key.(string); ok {
			ids = append(ids, id)
		}
		return true
	})
	assert.Equal(t, 2, len(ids))
	assert.Contains(t, ids, "client1.ID")
	assert.Contains(t, ids, "client2.ID")
	pool.Clear()
}
