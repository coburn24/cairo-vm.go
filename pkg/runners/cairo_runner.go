package runners

import (
	"fmt"

	"github.com/lambdaclass/cairo-vm.go/pkg/builtins"
	"github.com/lambdaclass/cairo-vm.go/pkg/lambdaworks"
	"github.com/lambdaclass/cairo-vm.go/pkg/layouts"
	"github.com/lambdaclass/cairo-vm.go/pkg/types"
	"github.com/lambdaclass/cairo-vm.go/pkg/utils"
	"github.com/lambdaclass/cairo-vm.go/pkg/vm"
	"github.com/lambdaclass/cairo-vm.go/pkg/vm/memory"
	"github.com/pkg/errors"
)

var ErrRunnerCalledTwice = errors.New("Cairo Runner was called twice")

type CairoRunner struct {
	Program               vm.Program
	Vm                    vm.VirtualMachine
	ProgramBase           memory.Relocatable
	executionBase         memory.Relocatable
	initialPc             memory.Relocatable
	initialAp             memory.Relocatable
	initialFp             memory.Relocatable
	finalPc               *memory.Relocatable
	mainOffset            uint
	ProofMode             bool
	RunEnded              bool
	Layout                layouts.CairoLayout
	execScopes            types.ExecutionScopes
	ExecutionPublicMemory *[]uint
	SegmentsFinalized     bool
}

func NewCairoRunner(program vm.Program, layoutName string, proofMode bool) (*CairoRunner, error) {
	mainIdentifier, ok := (program.Identifiers)["__main__.main"]
	main_offset := uint(0)
	if ok {
		main_offset = uint(mainIdentifier.PC)
	}

	err := utils.CheckBuiltinsSubsequence(program.Builtins)
	if err != nil {
		return nil, errors.New(err.Error())
	}

	var layout layouts.CairoLayout
	switch layoutName {
	case "plain":
		layout = layouts.NewPlainLayout()
	case "small":
		layout = layouts.NewSmallLayout()
	case "all_cairo":
		layout = layouts.NewAllCairoLayout()
	default:
		panic("Layout not implemented")
	}

	runner := CairoRunner{
		Program:    program,
		Vm:         *vm.NewVirtualMachine(),
		mainOffset: main_offset,
		ProofMode:  proofMode,
		Layout:     layout,
		execScopes: *types.NewExecutionScopes(),
	}
	return &runner, nil
}

// Performs the initialization step, returns the end pointer (pc upon which execution should stop)
func (r *CairoRunner) Initialize() (memory.Relocatable, error) {
	err := r.initializeBuiltins()
	if err != nil {
		return memory.Relocatable{}, errors.New(err.Error())
	}
	r.initializeSegments()
	end, err := r.initializeMainEntrypoint()
	if err == nil {
		err = r.initializeVM()
	}
	return end, err
}

// Initializes builtin runners in accordance to the specified layout and
// the builtins present in the running program.
func (r *CairoRunner) initializeBuiltins() error {
	var builtinRunners []builtins.BuiltinRunner
	programBuiltins := map[string]struct{}{}
	for _, builtin := range r.Program.Builtins {
		programBuiltins[builtin] = struct{}{}
	}

	for _, layoutBuiltin := range r.Layout.Builtins {
		_, included := programBuiltins[layoutBuiltin.Name()]
		if included {
			delete(programBuiltins, layoutBuiltin.Name())
			layoutBuiltin.Include(true)
			builtinRunners = append(builtinRunners, layoutBuiltin)
		} else if r.ProofMode {
			layoutBuiltin.Include(false)
			builtinRunners = append(builtinRunners, layoutBuiltin)
		}
	}

	if len(programBuiltins) != 0 {
		return errors.Errorf("Builtin(s) %v not present in layout %s", programBuiltins, r.Layout.Name)
	}

	r.Vm.BuiltinRunners = builtinRunners

	return nil
}

// Creates program, execution and builtin segments
func (r *CairoRunner) initializeSegments() {
	// Program Segment
	r.ProgramBase = r.Vm.Segments.AddSegment()
	// Execution Segment
	r.executionBase = r.Vm.Segments.AddSegment()
	// Builtin Segments
	for i := range r.Vm.BuiltinRunners {
		r.Vm.BuiltinRunners[i].InitializeSegments(&r.Vm.Segments)
	}
}

// Initializes the program segment & initial pc
func (r *CairoRunner) initializeState(entrypoint uint, stack *[]memory.MaybeRelocatable) error {
	r.initialPc = r.ProgramBase
	r.initialPc.Offset += entrypoint
	// Load program data
	_, err := r.Vm.Segments.LoadData(r.ProgramBase, &r.Program.Data)
	if err == nil {
		_, err = r.Vm.Segments.LoadData(r.executionBase, stack)
	}
	// Mark data segment as accessed
	base := r.ProgramBase
	var i uint
	for i = 0; i < uint(len(r.Program.Data)); i++ {
		r.Vm.Segments.Memory.MarkAsAccessed(memory.NewRelocatable(base.SegmentIndex, base.Offset+i))
	}
	return err
}

// Initializes memory, initial register values & returns the end pointer (final pc) to run from a given pc offset
// (entrypoint)
func (r *CairoRunner) initializeFunctionEntrypoint(entrypoint uint, stack *[]memory.MaybeRelocatable, return_fp memory.Relocatable) (memory.Relocatable, error) {
	end := r.Vm.Segments.AddSegment()
	*stack = append(*stack, *memory.NewMaybeRelocatableRelocatable(return_fp), *memory.NewMaybeRelocatableRelocatable(end))
	r.initialFp = r.executionBase
	r.initialFp.Offset += uint(len(*stack))
	r.initialAp = r.initialFp
	r.finalPc = &end
	return end, r.initializeState(entrypoint, stack)
}

// Initializes memory, initial register values & returns the end pointer (final pc) to run from the main entrypoint
func (r *CairoRunner) initializeMainEntrypoint() (memory.Relocatable, error) {
	// When running from main entrypoint, only up to 11 values will be written (9 builtin bases + end + return_fp)
	stack := make([]memory.MaybeRelocatable, 0, 11)
	// Append builtins initial stack to stack
	for i := range r.Vm.BuiltinRunners {
		for _, val := range r.Vm.BuiltinRunners[i].InitialStack() {
			stack = append(stack, val)
		}
	}

	if r.ProofMode {
		basePlusTwo := memory.NewRelocatable(r.executionBase.SegmentIndex, r.executionBase.Offset+2)
		stackPrefix := []memory.MaybeRelocatable{*memory.NewMaybeRelocatableRelocatable(basePlusTwo), *memory.NewMaybeRelocatableFelt(lambdaworks.FeltFromUint64(0))}

		stackPrefix = append(stackPrefix, stack...)

		var publicMemory []uint
		var i uint
		for i = 0; i < uint(len(stackPrefix)); i++ {
			publicMemory = append(publicMemory, i)
		}

		r.ExecutionPublicMemory = &publicMemory

		r.initializeState(r.Program.Start, &stackPrefix)

		initialFp := memory.NewRelocatable(r.executionBase.SegmentIndex, r.executionBase.Offset+2)
		r.initialFp = initialFp
		r.initialAp = r.initialFp

		return memory.NewRelocatable(r.ProgramBase.SegmentIndex, r.ProgramBase.Offset+r.Program.End), nil
	}

	return_fp := r.Vm.Segments.AddSegment()
	return r.initializeFunctionEntrypoint(r.mainOffset, &stack, return_fp)
}

// Initializes the vm's run_context, adds builtin validation rules & validates memory
func (r *CairoRunner) initializeVM() error {
	r.Vm.RunContext.Ap = r.initialAp
	r.Vm.RunContext.Fp = r.initialFp
	r.Vm.RunContext.Pc = r.initialPc
	// Add validation rules
	for i := range r.Vm.BuiltinRunners {
		r.Vm.BuiltinRunners[i].AddValidationRule(&r.Vm.Segments.Memory)
	}
	// Apply validation rules to memory
	return r.Vm.Segments.Memory.ValidateExistingMemory()
}

func (r *CairoRunner) BuildHintDataMap(hintProcessor vm.HintProcessor) (map[uint][]any, error) {
	hintDataMap := make(map[uint][]any, 0)
	for pc, hintsParams := range r.Program.Hints {
		hintDatas := make([]any, 0, len(hintsParams))
		for _, hintParam := range hintsParams {
			data, err := hintProcessor.CompileHint(&hintParam, &r.Program.ReferenceManager)
			if err != nil {
				return nil, err
			}
			hintDatas = append(hintDatas, data)
		}
		hintDataMap[pc] = hintDatas
	}

	return hintDataMap, nil
}

func (r *CairoRunner) RunUntilPC(end memory.Relocatable, hintProcessor vm.HintProcessor) error {
	hintDataMap, err := r.BuildHintDataMap(hintProcessor)
	if err != nil {
		return err
	}
	constants := r.Program.ExtractConstants()
	for r.Vm.RunContext.Pc != end {
		err := r.Vm.Step(hintProcessor, &hintDataMap, &constants, &r.execScopes)
		if err != nil {
			return err
		}
	}
	return nil
}

func (runner *CairoRunner) EndRun(disableTracePadding bool, disableFinalizeAll bool, vm *vm.VirtualMachine, hintProcessor vm.HintProcessor) error {
	if runner.RunEnded {
		return ErrRunnerCalledTwice
	}

	// TODO: This seems to have to do with temporary segments
	// vm.Segments.Memory.RelocateMemory()

	err := vm.EndRun()
	if err != nil {
		return err
	}

	if disableFinalizeAll {
		return nil
	}

	vm.Segments.ComputeEffectiveSizes()
	if runner.ProofMode && !disableTracePadding {
		err := runner.RunUntilNextPowerOfTwo(vm, hintProcessor)
		if err != nil {
			return err
		}

		for true {
			err := runner.CheckUsedCells(vm)
			if errors.Unwrap(err) == memory.ErrInsufficientAllocatedCells {
			} else if err != nil {
				return err
			} else {
				break
			}

			err = runner.RunForSteps(1, vm, hintProcessor)
			if err != nil {
				return err
			}

			err = runner.RunUntilNextPowerOfTwo(vm, hintProcessor)
			if err != nil {
				return err
			}
		}
	}

	runner.RunEnded = true
	return nil
}

func (r *CairoRunner) FinalizeSegments(virtualMachine vm.VirtualMachine) error {
	if r.SegmentsFinalized {
		return nil
	}

	if !r.RunEnded {
		return errors.New("Called Finalize Segments before run had ended")
	}

	var size = new(uint)
	*size = uint(len(r.Program.Data))
	var publicMemory []uint

	var i uint
	for i = 0; i < *size; i++ {
		publicMemory = append(publicMemory, i)
	}

	virtualMachine.Segments.Finalize(size, uint(r.ProgramBase.SegmentIndex), &publicMemory)

	publicMemory = make([]uint, 0)
	execBase := r.executionBase
	if r.ExecutionPublicMemory == nil {
		return errors.New("Called Finalized Segments without an Execution Public Memory")
	}

	for _, elem := range *r.ExecutionPublicMemory {
		publicMemory = append(publicMemory, elem+execBase.Offset)
	}

	virtualMachine.Segments.Finalize(nil, uint(execBase.SegmentIndex), &publicMemory)
	for _, builtin := range virtualMachine.BuiltinRunners {
		_, size, err := builtin.GetUsedCellsAndAllocatedSizes(&virtualMachine.Segments, virtualMachine.CurrentStep)
		if err != nil {
			return err
		}

		if builtin.Name() == builtins.OUTPUT_BUILTIN_NAME {
			var publicMemory []uint
			var i uint
			for i = 0; i < size; i++ {
				publicMemory = append(publicMemory, i)
			}
			virtualMachine.Segments.Finalize(&size, uint(builtin.Base().SegmentIndex), &publicMemory)
		} else {
			virtualMachine.Segments.Finalize(&size, uint(builtin.Base().SegmentIndex), nil)
		}
	}

	r.SegmentsFinalized = true
	return nil
}

func (r *CairoRunner) ReadReturnValues(virtualMachine *vm.VirtualMachine) error {
	if !r.RunEnded {
		return errors.New("Tried to read return values before run ended")
	}

	pointer := virtualMachine.RunContext.Ap

	for i := len(virtualMachine.BuiltinRunners) - 1; i >= 0; i-- {
		newPointer, err := virtualMachine.BuiltinRunners[i].FinalStack(&virtualMachine.Segments, pointer)
		if err != nil {
			return err
		}

		pointer = newPointer
	}

	if r.SegmentsFinalized {
		return errors.New("Failed Adding Return Values")
	}

	if r.ProofMode {
		execBase := r.executionBase
		begin := pointer.Offset - execBase.Offset

		ap := virtualMachine.RunContext.Ap
		end := ap.Offset - execBase.Offset

		var publicMemoryExtension []uint

		for i := begin; i < end; i++ {
			publicMemoryExtension = append(publicMemoryExtension, i)
		}

		*r.ExecutionPublicMemory = append(*r.ExecutionPublicMemory, publicMemoryExtension...)
	}

	return nil

}

func (runner *CairoRunner) CheckUsedCells(virtualMachine *vm.VirtualMachine) error {
	for _, builtin := range virtualMachine.BuiltinRunners {
		// I guess we call this just in case it errors out, even though later on we also call it?
		_, _, err := builtin.GetUsedCellsAndAllocatedSizes(&virtualMachine.Segments, virtualMachine.CurrentStep)
		if err != nil {
			return err
		}
	}

	err := runner.CheckRangeCheckUsage(virtualMachine)
	if err != nil {
		return err
	}

	err = runner.CheckMemoryUsage(virtualMachine)
	if err != nil {
		return err
	}

	err = runner.CheckDilutedCheckUsage(virtualMachine)
	if err != nil {
		return err
	}

	return nil
}

func (runner *CairoRunner) CheckMemoryUsage(virtualMachine *vm.VirtualMachine) error {
	instance := runner.Layout

	var builtinsMemoryUnits uint = 0

	for _, builtin := range virtualMachine.BuiltinRunners {
		result, err := builtin.GetAllocatedMemoryUnits(&virtualMachine.Segments, virtualMachine.CurrentStep)
		if err != nil {
			return err
		}

		builtinsMemoryUnits += result
	}

	totalMemoryUnits := instance.MemoryUnitsPerStep * virtualMachine.CurrentStep
	publicMemoryUnits := totalMemoryUnits / instance.PublicMemoryFraction
	remainder := totalMemoryUnits % instance.PublicMemoryFraction

	if remainder != 0 {
		return errors.Errorf("Total Memory units was not divisible by the Public Memory Fraction. TotalMemoryUnits: %d PublicMemoryFraction: %d", totalMemoryUnits, instance.PublicMemoryFraction)
	}

	instructionMemoryUnits := 4 * virtualMachine.CurrentStep
	unusedMemoryUnits := totalMemoryUnits - (publicMemoryUnits + instructionMemoryUnits + builtinsMemoryUnits)

	memoryAddressHoles, err := runner.GetMemoryHoles(virtualMachine)
	if err != nil {
		return err
	}

	if unusedMemoryUnits < memoryAddressHoles {
		return memory.InsufficientAllocatedCellsError(unusedMemoryUnits, memoryAddressHoles)
	}

	return nil
}

func (runner *CairoRunner) GetMemoryHoles(virtualMachine *vm.VirtualMachine) (uint, error) {
	return virtualMachine.Segments.GetMemoryHoles(uint(len(virtualMachine.BuiltinRunners)))
}

func (runner *CairoRunner) CheckDilutedCheckUsage(virtualMachine *vm.VirtualMachine) error {
	dilutedPoolInstance := runner.Layout.DilutedPoolInstance
	if dilutedPoolInstance == nil {
		return nil
	}

	var usedUnitsByBuiltins uint = 0

	for _, builtin := range virtualMachine.BuiltinRunners {
		usedUnits := builtin.GetUsedDilutedCheckUnits(dilutedPoolInstance.Spacing, dilutedPoolInstance.NBits)

		ratio := builtin.Ratio()
		if ratio == 0 {
			ratio = 1
		}
		multiplier, err := utils.SafeDiv(virtualMachine.CurrentStep, ratio)

		if err != nil {
			return err
		}

		usedUnitsByBuiltins += usedUnits * multiplier
	}

	var dilutedUnits uint = dilutedPoolInstance.UnitsPerStep * virtualMachine.CurrentStep
	var unusedDilutedUnits uint = dilutedUnits - usedUnitsByBuiltins

	var dilutedUsageUpperBound uint = 1 << dilutedPoolInstance.NBits

	if unusedDilutedUnits < dilutedUsageUpperBound {
		return memory.InsufficientAllocatedCellsError(unusedDilutedUnits, dilutedUsageUpperBound)
	}

	return nil
}

func (runner *CairoRunner) CheckRangeCheckUsage(virtualMachine *vm.VirtualMachine) error {
	var rcMin, rcMax *uint

	for _, builtin := range runner.Vm.BuiltinRunners {
		resultMin, resultMax := builtin.GetRangeCheckUsage(&runner.Vm.Segments.Memory)

		if resultMin != nil {
			rcMin = resultMin
		}

		if resultMax != nil {
			rcMax = resultMax
		}
	}

	if rcMin == nil || rcMax == nil {
		return nil
	}

	if runner.Vm.RcLimitsMax != nil && (uint(*runner.Vm.RcLimitsMax) > *rcMax) {
		*rcMax = uint(*runner.Vm.RcLimitsMax)
	}

	if runner.Vm.RcLimitsMin != nil && (uint(*runner.Vm.RcLimitsMin) < *rcMin) {
		*rcMin = uint(*runner.Vm.RcLimitsMin)
	}

	var rcUnitsUsedByBuiltins uint = 0

	for _, builtin := range runner.Vm.BuiltinRunners {
		usedUnits, err := builtin.GetUsedPermRangeCheckLimits(&virtualMachine.Segments, virtualMachine.CurrentStep)
		if err != nil {
			return err
		}

		rcUnitsUsedByBuiltins += usedUnits
	}

	unusedRcUnits := (runner.Layout.RcUnits-3)*virtualMachine.CurrentStep - uint(rcUnitsUsedByBuiltins)

	if unusedRcUnits < (*rcMax - *rcMin) {
		return memory.InsufficientAllocatedCellsError(unusedRcUnits, *rcMax-*rcMin)
	}

	return nil
}

// TODO: Add hint processor when it's done
func (runner *CairoRunner) RunForSteps(steps uint, virtualMachine *vm.VirtualMachine, hintProcessor vm.HintProcessor) error {
	hintDataMap, err := runner.BuildHintDataMap(hintProcessor)
	if err != nil {
		return err
	}
	constants := runner.Program.ExtractConstants()
	var remainingSteps int
	for remainingSteps = int(steps); remainingSteps > 0; remainingSteps-- {
		if runner.finalPc != nil && *runner.finalPc == virtualMachine.RunContext.Pc {
			return &vm.VirtualMachineError{Msg: fmt.Sprintf("EndOfProgram: %d", remainingSteps)}
		}

		err := virtualMachine.Step(hintProcessor, &hintDataMap, &constants, &runner.execScopes)
		if err != nil {
			return err
		}
	}

	return nil
}

// TODO: Add hint processor when it's done
func (runner *CairoRunner) RunUntilSteps(steps uint, virtualMachine *vm.VirtualMachine, hintProcessor vm.HintProcessor) error {
	return runner.RunForSteps(steps-virtualMachine.CurrentStep, virtualMachine, hintProcessor)
}

// TODO: Add hint processor when it's done
func (runner *CairoRunner) RunUntilNextPowerOfTwo(virtualMachine *vm.VirtualMachine, hintProcessor vm.HintProcessor) error {
	return runner.RunUntilSteps(utils.NextPowOf2(virtualMachine.CurrentStep), virtualMachine, hintProcessor)
}
