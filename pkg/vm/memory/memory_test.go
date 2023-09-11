package memory_test

import (
	"errors"
	"reflect"
	"testing"

	"github.com/lambdaclass/cairo-vm.go/pkg/builtins"
	"github.com/lambdaclass/cairo-vm.go/pkg/lambdaworks"
	"github.com/lambdaclass/cairo-vm.go/pkg/vm/memory"
)

// Misc validation rules for testing purposes
func rule_always_ok(mem *memory.Memory, ptr memory.Relocatable) ([]memory.Relocatable, error) {
	return []memory.Relocatable{ptr}, nil

}

func rule_always_err(mem *memory.Memory, ptr memory.Relocatable) ([]memory.Relocatable, error) {
	return nil, errors.New("Validation Failed")

}

func TestMemoryInsertWithValidationRulesOk(t *testing.T) {
	mem_manager := memory.NewMemorySegmentManager()
	mem_manager.AddSegment()
	mem := &mem_manager.Memory
	// Add a validation rule for segment 0
	mem.AddValidationRule(0, rule_always_ok)

	// Instantiate the address where we want to insert and the value.
	// We will insert the value Int(5) in segment 1, offset 0
	key := memory.NewRelocatable(0, 0)
	val := memory.NewMaybeRelocatableFelt(lambdaworks.FeltFromUint64(5))

	// Make the insertion
	err := mem.Insert(key, val)
	if err != nil {
		t.Errorf("Insert error in test: %s", err)
	}

	// Get the value from the address back
	res_val, err := mem.Get(key)
	if err != nil {
		t.Errorf("Get error in test: %s", err)
	}

	// Check that the original and the retrieved values are the same
	if !reflect.DeepEqual(res_val, val) {
		t.Errorf("Inserted value and original value are not the same")
	}
}
func TestMemoryInsertWithValidationRulesErr(t *testing.T) {
	mem_manager := memory.NewMemorySegmentManager()
	mem_manager.AddSegment()
	mem := &mem_manager.Memory
	// Add a validation rule for segment 0
	mem.AddValidationRule(0, rule_always_err)

	// Instantiate the address where we want to insert and the value.
	// We will insert the value Int(5) in segment 1, offset 0
	key := memory.NewRelocatable(0, 0)
	val := memory.NewMaybeRelocatableFelt(lambdaworks.FeltFromUint64(5))

	// Make the insertion
	err := mem.Insert(key, val)
	if err == nil {
		t.Errorf("Insertion should have failed due to validation rule")
	}
}

func TestMemoryInsert(t *testing.T) {
	mem_manager := memory.NewMemorySegmentManager()
	mem_manager.AddSegment()
	mem_manager.AddSegment()
	mem := &mem_manager.Memory

	// Instantiate the address where we want to insert and the value.
	// We will insert the value Felt(5) in segment 1, offset 0
	key := memory.NewRelocatable(1, 0)
	val := memory.NewMaybeRelocatableFelt(lambdaworks.FeltFromUint64(5))

	// Make the insertion
	err := mem.Insert(key, val)
	if err != nil {
		t.Errorf("Insert error in test: %s", err)
	}

	// Get the value from the address back
	res_val, err := mem.Get(key)
	if err != nil {
		t.Errorf("Get error in test: %s", err)
	}

	// Check that the original and the retrieved values are the same
	if !reflect.DeepEqual(res_val, val) {
		t.Errorf("Inserted value and original value are not the same")
	}
}

func TestMemoryInsertWithHoles(t *testing.T) {
	mem_manager := memory.NewMemorySegmentManager()
	mem_manager.AddSegment()
	mem_manager.AddSegment()
	mem := &mem_manager.Memory

	// Instantiate the address where we want to insert and the value.
	// We will insert the MaybeRelocatable Felt(7) in segment 1, offset 2
	key := memory.NewRelocatable(1, 2)
	val := memory.NewMaybeRelocatableFelt(lambdaworks.FeltFromUint64(5))

	// Make the insertion
	err := mem.Insert(key, val)
	if err != nil {
		t.Errorf("Insert error in test: %s", err)
	}

	// Get the value from the address back
	res_val, err := mem.Get(key)
	if err != nil {
		t.Errorf("Get error in test: %s", err)
	}

	// Check that the original and the retrieved values are the same
	if !reflect.DeepEqual(res_val, val) {
		t.Errorf("Inserted value and original value are not the same")
	}
}

func TestMemoryInsertOverWriteSameValue(t *testing.T) {
	mem_manager := memory.NewMemorySegmentManager()
	mem := &mem_manager.Memory

	// We will insert the MaybeRelocatable Felt(7) in segment 0, offset 0
	key := mem_manager.AddSegment()
	val := memory.NewMaybeRelocatableFelt(lambdaworks.FeltFromUint64(7))

	// Make the insertion
	err := mem.Insert(key, val)
	if err != nil {
		t.Errorf("Insert error in test: %s", err)
	}

	// Insert the same value again and check it doesn't fail
	err2 := mem.Insert(key, val)
	if err2 != nil {
		t.Errorf("Insert error in test: %s", err)
	}
}

func TestMemoryInsertOverWriteValue(t *testing.T) {
	mem_manager := memory.NewMemorySegmentManager()
	mem := &mem_manager.Memory

	// We will insert the MaybeRelocatable Felt(7) in segment 0, offset 0
	key := mem_manager.AddSegment()
	val := memory.NewMaybeRelocatableFelt(lambdaworks.FeltFromUint64(7))

	// Make the insertion
	err := mem.Insert(key, val)
	if err != nil {
		t.Errorf("Insert error in test: %s", err)
	}

	// Insert another value into the same address and check that it fails
	val2 := memory.NewMaybeRelocatableFelt(lambdaworks.FeltFromUint64(8))
	err2 := mem.Insert(key, val2)
	if err2 == nil {
		t.Errorf("Overwritting memory value should fail")
	}
}

func TestMemoryInsertUnallocatedSegment(t *testing.T) {
	mem_manager := memory.NewMemorySegmentManager()
	mem := &mem_manager.Memory

	// Instantiate the address where we want to insert and the value.
	// We will insert the value Felt(5) in segment 1, offset 0
	key := memory.NewRelocatable(1, 0)
	val := memory.NewMaybeRelocatableFelt(lambdaworks.FeltFromUint64(5))

	// Make the insertion
	err := mem.Insert(key, val)
	if err == nil {
		t.Errorf("Insertion on unallocated segment should fail")
	}
}

func TestMemorySegmentsLoadDataUnallocatedSegment(t *testing.T) {
	mem_manager := memory.NewMemorySegmentManager()

	ptr := memory.NewRelocatable(1, 0)
	data := []memory.MaybeRelocatable{*memory.NewMaybeRelocatableFelt(lambdaworks.FeltFromUint64(5))}

	// Load Data
	_, err := mem_manager.LoadData(ptr, &data)
	if err == nil {
		t.Errorf("Insertion on unallocated segment should fail")
	}
}

func TestMemorySegmentsLoadDataOneElement(t *testing.T) {
	mem_manager := memory.NewMemorySegmentManager()
	mem_manager.AddSegment()

	ptr := memory.NewRelocatable(0, 0)
	val := memory.NewMaybeRelocatableFelt(lambdaworks.FeltFromUint64(5))
	data := []memory.MaybeRelocatable{*val}

	// Load Data
	end_ptr, err := mem_manager.LoadData(ptr, &data)
	if err != nil {
		t.Errorf("LoadData error in test: %s", err)
	}

	// Check returned ptr
	expected_end_ptr := memory.NewRelocatable(0, 1)
	if !reflect.DeepEqual(end_ptr, expected_end_ptr) {
		t.Errorf("LoadData returned wrong ptr")
	}

	// Check inserted value
	res_val, err := mem_manager.Memory.Get(ptr)
	if err != nil {
		t.Errorf("Get error in test: %s", err)
	}

	// Check that the original and the retrieved values are the same
	if !reflect.DeepEqual(res_val, val) {
		t.Errorf("Inserted value and original value are not the same")
	}
}

func TestMemorySegmentsLoadDataTwoElements(t *testing.T) {
	mem_manager := memory.NewMemorySegmentManager()
	mem_manager.AddSegment()

	ptr := memory.NewRelocatable(0, 0)
	val := memory.NewMaybeRelocatableFelt(lambdaworks.FeltFromUint64(5))
	val2 := memory.NewMaybeRelocatableFelt(lambdaworks.FeltFromUint64(5))
	data := []memory.MaybeRelocatable{*val, *val2}

	// Load Data
	end_ptr, err := mem_manager.LoadData(ptr, &data)
	if err != nil {
		t.Errorf("LoadData error in test: %s", err)
	}

	// Check returned ptr
	expected_end_ptr := memory.NewRelocatable(0, 2)
	if !reflect.DeepEqual(end_ptr, expected_end_ptr) {
		t.Errorf("LoadData returned wrong ptr")
	}

	// Check inserted values

	// val
	res_val, err := mem_manager.Memory.Get(ptr)
	if err != nil {
		t.Errorf("Get error in test: %s", err)
	}

	// Check that the original and the retrieved values are the same
	if !reflect.DeepEqual(res_val, val) {
		t.Errorf("Inserted value and original value are not the same")
	}

	//val2
	ptr2 := memory.NewRelocatable(0, 1)
	res_val2, err := mem_manager.Memory.Get(ptr2)
	if err != nil {
		t.Errorf("Get error in test: %s", err)
	}

	// Check that the original and the retrieved values are the same
	if !reflect.DeepEqual(res_val2, val2) {
		t.Errorf("Inserted value and original value are not the same")
	}
}

func TestValidateExistingMemoryForRangeCheckWithinBounds(t *testing.T) {
	check_range := builtins.NewRangeCheckBuiltinRunner()
	segments := memory.NewMemorySegmentManager()
	check_range.InitializeSegments(&segments)
	check_range.AddValidationRule(&segments.Memory)

	for i := 0; i < 3; i++ {
		segments.AddSegment()
	}
	addr := memory.NewRelocatable(0, 0)
	val := memory.NewMaybeRelocatableFelt(lambdaworks.FeltFromUint64(45))
	err := segments.Memory.Insert(addr, val)
	if err != nil {
		t.Errorf("Insertion failed in test with error: %s", err)
	}

}

func TestValidateExistingMemoryForRangeCheckOutsideBounds(t *testing.T) {
	t.Helper()
	check_range := builtins.NewRangeCheckBuiltinRunner()
	segments := memory.NewMemorySegmentManager()
	segments.AddSegment()
	check_range.InitializeSegments(&segments)
	addr := memory.NewRelocatable(1, 0)
	val := memory.NewMaybeRelocatableFelt(lambdaworks.FeltFromDecString("-10"))
	segments.Memory.Insert(addr, val)
	check_range.AddValidationRule(&segments.Memory)
	err := segments.Memory.ValidateExistingMemory()
	expected_err := builtins.OutsideBoundsError(lambdaworks.FeltFromDecString("-10"))
	if err.Error() != expected_err.Error() {
		t.Errorf("This test should fail\n")
		t.Errorf("Expected: %s", expected_err)
		t.Errorf("Got: %s", err)
	}
}

func TestValidateExistingMemoryForRangeCheckRelocatableValue(t *testing.T) {
	check_range := builtins.NewRangeCheckBuiltinRunner()
	segments := memory.NewMemorySegmentManager()
	check_range.InitializeSegments(&segments)
	for i := 0; i < 3; i++ {
		segments.AddSegment()
	}
	addr := memory.NewRelocatable(0, 0)
	val := memory.NewMaybeRelocatableRelocatable(memory.NewRelocatable(0, 4))
	segments.Memory.Insert(addr, val)
	check_range.AddValidationRule(&segments.Memory)
	err := segments.Memory.ValidateExistingMemory()
	expected_err := builtins.NotAFeltError(addr, *val)
	if err.Error() != expected_err.Error() {
		t.Errorf("This test should fail")
		t.Errorf("Expected: %s", expected_err)
		t.Errorf("Got: %s", err)
	}
}

func TestValidateExistingMemoryForRangeCheckOutOfBoundsDiffSegment(t *testing.T) {
	check_range := builtins.NewRangeCheckBuiltinRunner()
	segments := memory.NewMemorySegmentManager()
	segments.AddSegment()
	check_range.InitializeSegments(&segments)

	addr := memory.NewRelocatable(0, 0)
	val := memory.NewMaybeRelocatableFelt(lambdaworks.FeltFromDecString("-45"))
	segments.Memory.Insert(addr, val)
	check_range.AddValidationRule(&segments.Memory)
	err := segments.Memory.ValidateExistingMemory()
	if err != nil {
		t.Errorf("This test should not return an error. Error: %s", err)
	}
}

func TestMemoryValidateExistingMemoryOk(t *testing.T) {
	mem_manager := memory.NewMemorySegmentManager()
	mem_manager.AddSegment()
	mem := &mem_manager.Memory
	// Load Values to memory
	for i := uint(0); i < 15; i++ {
		key := memory.NewRelocatable(0, i)
		val := memory.NewMaybeRelocatableFelt(lambdaworks.FeltFromUint64(uint64(i)))
		err := mem.Insert(key, val)
		if err != nil {
			t.Errorf("Insert error in test: %s", err)
		}
	}
	// Add a validation rule for segment 0
	mem.AddValidationRule(0, rule_always_ok)
	// Run ValidateExistingMemory
	err := mem.ValidateExistingMemory()
	if err != nil {
		t.Errorf("ValidateExistingMemory error in test: %s", err)
	}
}

func TestMemoryValidateExistingMemoryErr(t *testing.T) {
	mem_manager := memory.NewMemorySegmentManager()
	mem_manager.AddSegment()
	mem := &mem_manager.Memory
	// Load Values to memory
	for i := uint(0); i < 15; i++ {
		key := memory.NewRelocatable(0, i)
		val := memory.NewMaybeRelocatableFelt(lambdaworks.FeltFromUint64(uint64(i)))
		err := mem.Insert(key, val)
		if err != nil {
			t.Errorf("Insert error in test: %s", err)
		}
	}
	// Add a validation rule for segment 0
	mem.AddValidationRule(0, rule_always_err)
	// Run ValidateExistingMemory
	err := mem.ValidateExistingMemory()
	if err == nil {
		t.Errorf("ValidateExistingMemory should have failed")
	}
}
