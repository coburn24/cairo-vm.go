# cairo-vm.go

This is a work in progress implementation of the [Cairo VM](https://github.com/lambdaclass/cairo-vm) in `Go`. The reasons for doing this include:

- Having a diversity of implementations helps find bugs and make the whole ecosystem more resilient.
- It's a good opportunity to extensively document the VM in general, as currently the documentation on its internals is very scarce and mostly lives on the minds of a few people.

## Other docs

- [Project layout](docs/layout.md)
- [Rust/lambdaworks integration](docs/rust-integration.md)

## Installation

Go needs to be installed. For mac computers, run

```shell
brew install go
```

We also use [pyenv](https://github.com/pyenv/pyenv) to install testing-related dependencies

## Compiling, running, testing

First, install the testing dependencies:

- On Linux, run

```shell
make deps
```

- On MacOS, run

```shell
make deps-macos
```

To build the project, run:

```shell
make build
```

To run all tests, activate the venv created by make deps and run the test target:

```shell
. cairo-vm-env/bin/activate
make test
```

## Running the demo

This project currently has two demo targets, one for running a fibonacci programs and one for running a factorial program. Both of them output their corresponding trace files.
The demo uses cairo_lang to compile both cairo programs, you can install it by running `make deps` (or `make deps-macos` if you are on macos)

To run the fibonacci demo:

```shell
make demo_fibonacci
```

To run the factorial demo:

```shell
make demo_factorial
```

## Project Guidelines

- PRs addressing performance are forbidden. We are currently concerned with making it work without bugs and nothing more.
- All PRs must contain tests. Code coverage has to be above 98%.
- To check for security and other types of bugs, the code will be fuzzed extensively.
- PRs must be accompanied by its corresponding documentation. A book will be written documenting the entire inner workings of it, so anyone can dive in to a Cairo VM codebase and follow it along.

## Milestones

### First milestone: Fibonacci/Factorial

The first milestone for Cairo VM in Go is completed! 🎉

The milestone includes:

- Parsing of `json` programs.
- Decoding of instructions.
- Memory relocation.
- Instruction execution.
- Writing of the trace into files with the correct format.
- Make the fibonacci and factorial tests pass, comparing our own trace with the Rust VM one, making sure they match.
- Writing of the memory into files with the correct format.
- Make the fibonacci and factorial tests pass, comparing our own memory with the Rust VM one, making sure they match.

## Roadmap

### Cairo 0/Cairo 1

The above will work for Cairo 0 programs. Cairo 1 has the following extra issues to address:

- There is no `Json` representation of a Cairo 1 Program, so we can only run contracts. This means we will have to add functions to run cairo contracts (aka implement run_from_entrypoint).
- Cairo 1 contracts use the `range_check` builtin by default, so we need to implement it.

### Full VM implementation

To have a full implementation, we will need the following:

- Builtins. Add the `BuiltinRunner` logic, then implement each builtin:
  - `Range check (RC)`
  - `Output`
  - `Bitwise`
  - `Ec_op`
  - `Pedersen`
  - `Keccak`
  - `Poseidon`
  - `Signature`
  - `Segment Arena`
- Memory layouts. This is related to builtins but we will do it after implementing them.
- Hints. Add the `HintProcessor` logic, then implement each hint. Hints need to be documented extensively, implementing them is super easy since it's just porting code; what's not so clear is what they are used for. Having explanations and examples for each is important. List of hints below:
  - [Parsing of references](https://github.com/lambdaclass/cairo-vm/tree/main/docs/references_parsing)
  - [`CommonLib`](https://github.com/starkware-libs/cairo-lang/tree/master/src/starkware/cairo/common)
  - `Secp`
  - `Vrf`
  - `BigInt`
  - `Blake2`
  - `DictManager`
  - `EcRecover`
  - `Field Arithmetic`
  - `Garaga`
  - `set_add`
  - `sha256 utils`
  - `ECDSA verification`
  - `uint384` and `uint384 extension`
  - `uint512 utils`
  - `Cairo 1` hints.
- Proof mode. It's important to explain in detail what this is when we do it. It's one of the most obscure parts of the VM in my experience.
- Air Public inputs. (Tied to Proof-mode)
- Temporary segments.
- Program tests from Cairo VM in Rust.
- Fuzzing/Differential fuzzing.

## Documentation

### High Level Overview

The Cairo virtual machine is meant to be used in the context of STARK validity proofs. What this means is that the point of Cairo is not just to execute some code and get a result, but to *prove* to someone else that said execution was done correctly, without them having to re-execute the entire thing. The rough flow for it looks like this:

- A user writes a Cairo program.
- The program is compiled into Cairo's VM bytecode.
- The VM executes said code and provides a *trace* of execution, i.e. a record of the state of the machine and its memory *at every step of the computation*.
- This trace is passed on to a STARK prover, which creates a cryptographic proof from it, attesting to the correct execution of the program.
- The proof is passed to a verifier, who checks that the proof is valid in a fraction of a second, without re-executing.

The main three components of this flow are:

- A Cairo compiler to turn a program written in the [Cairo programming language](https://www.cairo-lang.org/) into bytecode.
- A Cairo VM to then execute it and generate a trace.
- [A STARK prover and verifier](https://github.com/lambdaclass/starknet_stack_prover_lambdaworks) so one party can prove correct execution, while another can verify it.

While this repo is only concerned with the second component, it's important to keep in mind the other two; especially important are the prover and verifier that this VM feeds its trace to, as a lot of its design decisions come from them. This virtual machine is designed to make proving and verifying both feasible and fast, and that makes it quite different from most other VMs you are probably used to.

### Basic VM flow

Our virtual machine has a very simple flow:

- Take a compiled cairo program as input. You can check out an example program [here](https://github.com/lambdaclass/cairo-vm.go/blob/main/cairo_programs/fibonacci.cairo).
- Run the bytecode from the compiled program, doing the usual `fetch->decode->execute` loop, running until program termination.
- On every step of the execution, record the values of each register.
- Take the register values and memory at every step and write them to a file, called the `execution trace`.

Barring some simplifications we made, this is all the Cairo VM does. The two main things that stand out as radically different are the memory model and the use of `Field Elements` to perform arithmetic. Below we go into more detail on each step, and in the process explain the ommisions we made.

### Architecture

The Cairo virtual machine uses a Von Neumann architecture with a Non-deterministic read-only memory. What this means, roughly, is that memory is immutable after you've written to it (i.e. you can only write to it once); this is to make the STARK proving easier, but we won't go into that here.

#### Memory Segments and Relocation

The process of memory allocation in a contiguous write-once memory region can get pretty complicated. Imagine you want to have a regular call stack, with a stack pointer pointing to the top of it and allocation and deallocation of stack frames and local variables happening throughout execution. Because memory is immutable, this cannot be done the usual way; once you allocate a new stack frame that memory is set, it can't be reused for another one later on.

Because of this, memory in Cairo is divided into `segments`. This is just a way of organizing memory more conveniently for this write-once model. Each segment is nothing more than a contiguous memory region. Segments are identified by an `index`, an integer value that uniquely identifies them.

Memory `cells` (i.e. values in memory) are identified by the index of the segment they belong to and an `offset` into said segment. Thus, the memory cell `{2,0}` is the first cell of segment number `2`.

Even though this segment model is extremely convenient for the VM's execution, the STARK prover needs to have the memory as just one contiguous region. Because of this, once execution of a Cairo program finishes, all the memory segments are collapsed into one; this process is called `Relocation`. We will go into more detail on all of this below.

#### Registers

There are only three registers in the Cairo VM:

- The program counter `pc`, which points to the next instruction to be executed.
- The allocation pointer `ap`, pointing to the next unused memory cell.
- The frame pointer `fp`, pointing to the base of the current stack frame. When a new function is called, `fp` is set to the current `ap`. When the function returns, `fp` goes back to its previous value. The VM creates new segments whenever dynamic allocation is needed, so for example the cairo analog to a Rust `Vec` will have its own segment. Relocation at the end meshes everything together.

#### Instruction Decoding/Execution

TODO: explain the components of an instruction (`dst_reg`, `op0_reg`, etc), what each one is used for and how they're encoded/decoded.

#### Felts

Felts, or Field Elements, are cairo's basic integer type. Every variable in a cairo vm that is not a pointer is a felt. From our point of view we could say a felt in cairo is an unsigned integer in the range [0, CAIRO_PRIME). This means that all operations are done modulo CAIRO_PRIME. The CAIRO_PRIME is 0x800000000000011000000000000000000000000000000000000000000000001, which means felts can be quite big (up to 252 bits), luckily, we have the [Lambdaworks](https://github.com/lambdaclass/lambdaworks) library to help with handling these big integer values and providing fast and efficient modular arithmetic.

#### Lambdaworks library wrapper

[Lambdaworks](https://github.com/lambdaclass/lambdaworks) is a custom performance-focused library that aims to ease programming for developers. It provides essential mathematical and cryptographic methods required for this project, enabling arithmetic operations between `felts` and type conversions efficiently.
We've developed a C wrapper to expose the library's functions and enable easy usage from Go. This allows seamless integration of the library's features within Go projects, enhancing performance and functionality.

#### More on memory

The cairo memory is made up of contiguous segments of variable length identified by their index. The first segment (index 0) is the program segment, which stores the instructions of a cairo program. The following segment (index 1) is the execution segment, which holds the values that are created along the execution of the vm, for example, when we call a function, a pointer to the next instruction after the call instruction will be stored in the execution segment which will then be used to find the next instruction after the function returns. The following group of segments are the builtin segments, one for each builtin used by the program, and which hold values used by the builtin runners. The last group of segments are the user segments, which represent data structures created by the user, for example, when creating an array on a cairo program, that array will be represented in memory as its own segment.

An address (or pointer) in cairo is represented as a `relocatable` value, which is made up of a `segment_index` and an `offset`, the `segment_index` tells us which segment the value is stored in and the `offset` tells us how many values exist between the start of the segment and the value.

As the cairo memory can hold both felts and pointers, the basic memory unit is a `maybe_relocatable`, a variable that can be either a `relocatable` or a `felt`.

While memory is continous, some gaps may be present. These gaps can be created on purpose by the user, for example by running:

```text
[ap + 1] = 2;
```

Where a gap is created at ap. But they may also be created indireclty by diverging branches, as for example one branch may declare a variable that the other branch doesn't, as memory needs to be allocated for both cases if the second case is ran then a gap is left where the variable should have been written.

##### Memory API

The memory can perform the following basic operations:

- `memory_add_segment`: Creates a new, empty segment in memory and returns a pointer to its start. Values cannot be inserted into a memory segment that hasn't been previously created.

- `memory_insert`: Inserts a `maybe_relocatable` value at an address indicated by a `relocatable` pointer. For this operation to succeed, the pointer's segment_index must be an existing segment (created using `memory_add_segment`), and there mustn't be a value stored at that address, as the memory is immutable after its been written once. If there is a value already stored at that address but it is equal to the value to be inserted then the operation will be successful.

- `memory_get`: Fetches a `maybe_relocatable` value from a memory address indicated by a `relocatable` pointer.

Other operations:

- `memory_load_data`: This is a convenience method, which takes an array of `maybe_relocatable` and inserts them contiguosuly in memory by calling `memory_insert` and advancing the pointer by one after each insertion. Returns a pointer to the next free memory slot after the inserted data.

##### Memory Relocation

During execution, the memory consists of segments of varying length, and they can be accessed by indicating their segment index, and the offset within that segment. When the run is finished, a relocation process takes place, which transforms this segmented memory into a contiguous list of values. The relocation process works as follows:

1. The size of each segment is calculated (The size is equal to the highest offset within the segment + 1, and not the amount of `maybe_relocatable` values, as there can be gaps)
2. A base is assigned to each segment by accumulating the size of the previous segment. The first segment's base is set to 1.
3. All `relocatable` values are converted into a single integer by adding their `offset` value to their segment's base calculated in the previous step

For example, if we have this memory represented by address, value pairs:

```text
    0:0 -> 1
    0:1 -> 4
    0:2 -> 7
    1:0 -> 8
    1:1 -> 0:2
    1:4 -> 0:1
    2:0 -> 1
```

Step 1: Calculate segment sizes:

```text
    0 --(has size)--> 3
    1 --(has size)--> 5
    2 --(has size)--> 1
```

Step 2: Assign a base to each segment:

```text
    0 --(has base value)--> 1
    1 --(has base value)--> 4 (that is: 1 + 3)
    2 --(has base value)--> 9 (that is: 4 + 5)
```

Step 3: Convert relocatables to integers

```text
    1 (base[0] + 0) -> 1
    2 (base[0] + 1) -> 4
    3 (base[0] + 2) -> 7
    4 (base[1] + 0) -> 8
    5 (base[1] + 1) -> 3 (that is: base[0] + 2)
    .... (memory gaps)
    8 (base[1] + 4) -> 2 (that is: base[0] + 1)
    9 (base[2] + 0) -> 1
```

#### Program parsing

The input of the Virtual Machine is a compiled Cairo program in Json format. The main part of the file are listed below:

- **data:** List of hexadecimal values that represent the instructions and immediate values defined in the cairo program. Each hexadecimal value is stored as a maybe_relocatable element in memory, but they can only be felts because the decoder has to be able to get the instruction fields in its bit representation.
- **debug_info:** This field provides information about the instructions defined in the data list. Each one is identified with its index inside the data list. For each one it contains information about the cairo variables in scope, the hints executed before that instruction if any, and its location inside the cairo program.
- **hints:** All the hints used in the program, ordered by the pc offset at which they should be executed.
- **identifiers:** User-defined symbols in the Cairo code representing variables, functions, classes, etc. with unique names. The expected offset, type and its corresponding information is provided for each identifier

    For example, the identifier representing the main function (usually the entrypoint of the program) is of `function` type, and a list of decorators wrappers (if there are any) are provided as additional information.
    Another example is a user defined struct, is of `struct` type, it provides its size, the members it contains (with its information) and more.

- **main_scope:** Usually something like `__main__`. All the identifiers associated with main function will be identified as `__main__`.identifier_name. Useful to identify the entrypoint of the program.
- **prime:** The cairo prime in hexadecimal format. As explained above, all arithmetic operations are done over a base field, modulo this primer number.
- **reference_manager:** Contains information about cairo variables. This information is useful to access to variables when executing cairo hints.

### Code walkthrough/Write your own Cairo VM

Let's begin by creating the basic types and structures for our VM:

#### Felt

As anyone who has ever written a cairo program will know, everything in cairo is a Felt. We can think of it as our unsigned integer. In this project, we use the `Lambdaworks` library to abstract ourselves from modular arithmetic.

TODO: Instructions on how to use Lambdaworks felt from Go

#### Relocatable

This is how cairo represents pointers, they are made up of `SegmentIndex`, which segment the variable is in, and `Offset`, how many values exist between the start of a segment and the variable. We represent them like this:

```go
type Relocatable struct {
    SegmentIndex int
    Offset       uint
}
```

#### MaybeRelocatable

As the cairo memory can hold both felts and relocatables, we need a data type that can represent both in order to represent a basic memory unit.
We would normally use enums or unions to represent this type, but as go lacks both, we will instead hold a non-typed inner value and rely on the api to make sure we can only create MaybeRelocatable values with either Felt or Relocatable as inner type.

```go
type MaybeRelocatable struct {
    inner any
}

// Creates a new MaybeRelocatable with an Int inner value
func NewMaybeRelocatableInt(felt uint) *MaybeRelocatable {
    return &MaybeRelocatable{inner: Int{felt}}
}

// Creates a new MaybeRelocatable with a Relocatable inner value
func NewMaybeRelocatableRelocatable(relocatable Relocatable) *MaybeRelocatable {
    return &MaybeRelocatable{inner: relocatable}
}
```

We will also add some methods that will allow us access `MaybeRelocatable` inner values:

```go
// If m is Int, returns the inner value + true, if not, returns zero + false
func (m *MaybeRelocatable) GetInt() (Int, bool) {
    int, is_type := m.inner.(Int)
    return int, is_type
}

// If m is Relocatable, returns the inner value + true, if not, returns zero + false
func (m *MaybeRelocatable) GetRelocatable() (Relocatable, bool) {
    rel, is_type := m.inner.(Relocatable)
    return rel, is_type
}
```

These will allow us to safely discern between `Felt` and `Relocatable` values later on.

#### MaybeRelocatable Operations

Introducing the MaybeRelocatable type means we will have to handle various arithmetic operations between Relocatable, Felt and MaybeRelocatable types.
We will start by implementing Add and Sub operations for the `Relocatable` type:

##### Relocatable.Add

Addition between Relocatable values is not supported, so we don't implement it.

##### Relocatable.AddFelt

This method adds a Felt value to the relocatable's offset by first converting the relocatable's offset to a Felt, performing felt addition between the offset and the felt value, and then converting the new offset to a uint value. This method returns an error if the new offset exceeds the size of a uint.

```go
// Adds a Felt value to a Relocatable
// Fails if the new offset exceeds the size of a uint
func (r *Relocatable) AddFelt(other lambdaworks.Felt) (Relocatable, error) {
 new_offset_felt := lambdaworks.FeltFromUint64(uint64(r.Offset)).Add(other)
 new_offset, err := new_offset_felt.ToU64()
 if err != nil {
  return *r, err
 }
 return NewRelocatable(r.SegmentIndex, uint(new_offset)), nil
}
```

##### Relocatable.Sub

This method returns the distance between two relocatable values. It can only be performed between to relocatables of the same segment (aka relocatables with the same segment index), and it returns the difference between their offsets as a uint value. It fails if the segment indexes differ or if the difference would yield a negative value

```go
// Returns the distance between two relocatable values (aka the difference between their offsets)
// Fails if they have different segment indexes or if the difference is negative
func (r *Relocatable) Sub(other Relocatable) (uint, error) {
 if r.SegmentIndex != other.SegmentIndex {
  return 0, errors.New("Cant subtract two relocatables with different segment indexes")
 }
 if r.Offset < other.Offset {
  return 0, errors.New("Relocatable subtraction yields relocatable with negative offset")
 }
 return r.Offset - other.Offset, nil
}
```

##### Relocatable.SubFelt

This method subtracts a Felt value to the relocatable's offset by first converting the relocatable's offset to a Felt, performing felt subtraction between the offset and the felt value, and then converting the new offset to a uint value. This method returns an error if the new offset is negative or exceeds the size of a uint.

```go
// Substracts a Felt value from a Relocatable
// Performs the initial substraction considering the offset as a Felt
// Fails if the new offset exceeds the size of a uint
func (r *Relocatable) SubFelt(other lambdaworks.Felt) (Relocatable, error) {
 new_offset_felt := lambdaworks.FeltFromUint64(uint64(r.Offset)).Sub(other)
 new_offset, err := new_offset_felt.ToU64()
 if err != nil {
  return *r, err
 }
 return NewRelocatable(r.SegmentIndex, uint(new_offset)), nil
}
```

Now lets look at the operations between `MaybeRelocatable`s:

##### MaybeRelocatable.Add

There are four different cases to consider when adding two `MaybeRelocatable` values:

1. Both values are `Felt`: We perform felt addition
2. Both values are `Relocatable`: This operation is not supported so we return an error
3. First value is `Felt` and other in `Relocatable`: This operation is not supported so we return an error
4. First value is `Relocatable` and other is `Felt`: We call `Relocatable.AddFelt`

```go
func (m MaybeRelocatable) Add(other MaybeRelocatable) (MaybeRelocatable, error) {
 // check if they are felt
 m_int, m_is_int := m.GetFelt()
 other_int, other_is_int := other.GetFelt()

 if m_is_int && other_is_int {
  result := NewMaybeRelocatableFelt(m_int.Add(other_int))
  return *result, nil
 }

 // check if one is relocatable and the other int
 m_rel, is_rel_m := m.GetRelocatable()
 other_rel, is_rel_other := other.GetRelocatable()

 if is_rel_m && !is_rel_other {
  other_felt, _ := other.GetFelt()
  relocatable, err := m_rel.AddFelt(other_felt)
  if err != nil {
   return *NewMaybeRelocatableFelt(lambdaworks.FeltZero()), err
  }
  return *NewMaybeRelocatableRelocatable(relocatable), nil

 } else if !is_rel_m && is_rel_other {

  m_felt, _ := m.GetFelt()
  relocatable, err := other_rel.AddFelt(m_felt)
  if err != nil {
   return *NewMaybeRelocatableFelt(lambdaworks.FeltZero()), err
  }
  return *NewMaybeRelocatableRelocatable(relocatable), nil
 } else {
  return *NewMaybeRelocatableFelt(lambdaworks.FeltZero()), errors.New("RelocatableAdd")
 }
}
```

##### MaybeRelocatable.Sub

There are four different cases to consider when adding two `MaybeRelocatable` values:

1. Both values are `Felt`: We perform felt subtraction
2. Both values are `Relocatable`: We call `Relocatable.Sub`
3. First value is `Felt` and other in `Relocatable`: This operation is not supported so we return an error
4. First value is `Relocatable` and other is `Felt`: We call `Relocatable.SubFelt`

```go
func (m MaybeRelocatable) Sub(other MaybeRelocatable) (MaybeRelocatable, error) {
 // check if they are felt
 m_int, m_is_int := m.GetFelt()
 other_felt, other_is_felt := other.GetFelt()

 if m_is_int && other_is_felt {
  result := NewMaybeRelocatableFelt(m_int.Sub(other_felt))
  return *result, nil
 }

 // check if one is relocatable and the other int
 m_rel, is_rel_m := m.GetRelocatable()
 other_rel, is_rel_other := other.GetRelocatable()

 if is_rel_m && !is_rel_other {
  relocatable, err := m_rel.SubFelt(other_felt)
  if err != nil {
   return *NewMaybeRelocatableFelt(lambdaworks.FeltZero()), err
  }
  return *NewMaybeRelocatableRelocatable(relocatable), nil

 } else if is_rel_m && is_rel_other {
  offset_diff, err := m_rel.Sub(other_rel)
  if err != nil {
   return *NewMaybeRelocatableFelt(lambdaworks.FeltZero()), err
  }
  return *NewMaybeRelocatableFelt(lambdaworks.FeltFromUint64(uint64(offset_diff))), nil
 } else {
  return *NewMaybeRelocatableFelt(lambdaworks.FeltZero()), errors.New("Cant sub Relocatable from Felt")
 }
}
```

#### Memory

As we previously described, the memory is made up of a series of segments of variable length, each containing a continuous sequence of `MaybeRelocatable` elements. Memory is also immutable, which means that once we have written a value into memory, it can't be changed.
There are multiple valid ways to represent this memory structure, but the simplest way to represent it is by using a map, maping a `Relocatable` address to a `MaybeRelocatable` value.
As we don't have an actual representation of segments, we have to keep track of the number of segments.

```go
type Memory struct {
    data         map[Relocatable]MaybeRelocatable
    num_segments uint
}
```

Now we can define the basic memory operations:

##### Insert

Here we need to make perform some checks to make sure that the memory remains consistent with its rules:

- We must check that insertions are performed on previously-allocated segments, by checking that the address's segment_index is lower than our segment counter
- We must check that we are not mutating memory we have previously written, by checking that the memory doesn't already contain a value at that address that is not equal to the one we are inserting

```go
func (m *Memory) Insert(addr Relocatable, val *MaybeRelocatable) error {
    // Check that insertions are preformed within the memory bounds
    if addr.segmentIndex >= int(m.num_segments) {
        return errors.New("Error: Inserting into a non allocated segment")
    }

    // Check for possible overwrites
    prev_elem, ok := m.data[addr]
    if ok && prev_elem != *val {
        return errors.New("Memory is write-once, cannot overwrite memory value")
    }

    m.data[addr] = *val

    return nil
}
```

##### Get

This is the easiest operation, as we only need to fetch the value from our map:

```go
// Gets some value stored in the memory address `addr`.
func (m *Memory) Get(addr Relocatable) (*MaybeRelocatable, error) {
    value, ok := m.data[addr]

    if !ok {
        return nil, errors.New("Memory Get: Value not found")
    }

    return &value, nil
}
```

#### MemorySegmentManager

In our `Memory` implementation, it looks like we need to have segments allocated before performing any valid memory operation, but we can't do so from the `Memory` api. To do so, we need to use the `MemorySegmentManager`.
The `MemorySegmentManager` is in charge of creating new segments and calculating their size during the relocation process, it has the following structure:

```go
type MemorySegmentManager struct {
    segmentSizes map[uint]uint
    Memory       Memory
}
```

And the following methods:

##### Add Segment

As we are using a map, we dont have to allocate memory for the new segment, so we only have to raise our segment counter and return the first address of the new segment:

```go
func (m *MemorySegmentManager) AddSegment() Relocatable {
    ptr := Relocatable{int(m.Memory.num_segments), 0}
    m.Memory.num_segments += 1
    return ptr
}
```

##### Load Data

This method inserts a contiguous array of values starting from a certain addres in memory, and returns the next address after the inserted values. This is useful when inserting the program's instructions in memory.
In order to perform this operation, we only need to iterate over the array, inserting each value at the address indicated by `ptr` while advancing the ptr with each iteration and then return the final ptr.

```go
func (m *MemorySegmentManager) LoadData(ptr Relocatable, data *[]MaybeRelocatable) (Relocatable, error) {
    for _, val := range *data {
        err := m.Memory.Insert(ptr, &val)
        if err != nil {
            return Relocatable{0, 0}, err
        }
        ptr.offset += 1
    }
    return ptr, nil
}
```

#### RunContext

The RunContext keeps track of the vm's registers. Cairo VM only has 3 registers:

- The program counter `Pc`, which points to the next instruction to be executed.
- The allocation pointer `Ap`, pointing to the next unused memory cell.
- The frame pointer `Fp`, pointing to the base of the current stack frame. When a new function is called, `Fp` is set to the current `Ap` value. When the function returns, `Fp` goes back to its previous value.

We can represent it like this:

```go
type RunContext struct {
    Pc memory.Relocatable
    Ap memory.Relocatable
    Fp memory.Relocatable
}
```

#### VirtualMachine

With all of these types and structures defined, we can build our VM:

```go
type VirtualMachine struct {
    RunContext     RunContext
    currentStep    uint
    Segments       memory.MemorySegmentManager
}
```

To begin coding the basic execution functionality of our VM, we only need these basic fields, we will be adding more fields as we dive deeper into this guide.

#### Instruction Decoding and Execution

Cairo program execution is divided into steps, and in turn each step is divided into:
  1. Instruction decoding
  2. Instruction execution

##### Step 

This method is the organizer of the execution of each instruction, it orchestrates them and handles the possible errors. 

The first thing it does is to obtain the instruction we want to run, it does that by getting the value on memory where the current pc is pointing. We know that the instruction has to be a felt, if it is not, then there is an error with the encoding of the instruction. 
Once we retrieve the felt we have the `encoded instruction`, we need to decode it to get the fields from its bits representation. Felt is not useful anymore so we will get its integer representation. 
Now it's time to decode the instruction and then run the `decoded instruction`.


```go
func (v *VirtualMachine) Step() error {
	encoded_instruction, err := v.Segments.Memory.Get(v.RunContext.Pc)
	if err != nil {
		return fmt.Errorf("Failed to fetch instruction at %+v", v.RunContext.Pc)
	}

	encoded_instruction_felt, ok := encoded_instruction.GetFelt()
	if !ok {
		return errors.New("Wrong instruction encoding")
	}

	encoded_instruction_uint, err := encoded_instruction_felt.ToU64()
	if err != nil {
		return err
	}

	instruction, err := DecodeInstruction(encoded_instruction_uint)
	if err != nil {
		return err
	}

	return v.RunInstruction(&instruction)
}
```

##### Decode instruction 

```go
//  Structure of the 63-bit that form the first word of each instruction.
//  See Cairo whitepaper, page 32 - https://eprint.iacr.org/2021/1063.pdf.
// ┌─────────────────────────────────────────────────────────────────────────┐
// │                     off_dst (biased representation)                     │
// ├─────────────────────────────────────────────────────────────────────────┤
// │                     off_op0 (biased representation)                     │
// ├─────────────────────────────────────────────────────────────────────────┤
// │                     off_op1 (biased representation)                     │
// ├─────┬─────┬───────┬───────┬───────────┬────────┬───────────────────┬────┤
// │ dst │ op0 │  op1  │  res  │    pc     │   ap   │      opcode       │ 0  │
// │ reg │ reg │  src  │ logic │  update   │ update │                   │    │
// ├─────┼─────┼───┬───┼───┬───┼───┬───┬───┼───┬────┼────┬────┬────┬────┼────┤
// │  0  │  1  │ 2 │ 3 │ 4 │ 5 │ 6 │ 7 │ 8 │ 9 │ 10 │ 11 │ 12 │ 13 │ 14 │ 15 │
// └─────┴─────┴───┴───┴───┴───┴───┴───┴───┴───┴────┴────┴────┴────┴────┴────┘
```

As we can see in the chart above, all the information we need is present on the bits representation of the instruction. The first thing to do is create a structure that stores it. 

```go 
type Instruction struct {
	Off0     int
	Off1     int
	Off2     int
	DstReg   Register
	Op0Reg   Register
	Op1Addr  Op1Src
	ResLogic ResLogic
	PcUpdate PcUpdate
	ApUpdate ApUpdate
	FpUpdate FpUpdate
	Opcode   Opcode
}
```

And the good thing about this is that every combination of bits for each field is known, so we can code all the possible flags to work with. These flags are represented below. 

The off0, off1, and off2 values are used to compute the address of the dst, op0 and op1 respectively.
For example, if the DstReg is AP and Off0 is -1, then we can compute the dst address by substracting one from the current value of ap.

There's two possible registers, ap and fp. The ap register (address pointer register) keeps track of memory addresses for data access. The fp register (frame pointer register) manages function call stack frames, local variables and parameter access.

```go 
type Register uint

const (
	AP Register = 0
	FP Register = 1
)
```

The Op1Src constants define sources for an operation, including immediate values, registers (ap, fp), and an operation result.

```go
type Op1Src uint

const (
	Op1SrcImm Op1Src = 0
	Op1SrcAP  Op1Src = 1
	Op1SrcFP  Op1Src = 2
	Op1SrcOp0 Op1Src = 4
)
```
The ResLogic constants represent different types of results in a program, including operation results, addition, multiplication, and unconstrained values.

```go
type ResLogic uint

const (
	ResOp1           ResLogic = 0
	ResAdd           ResLogic = 1
	ResMul           ResLogic = 2
	ResUnconstrained ResLogic = 3
)
```

The PcUpdate constants define different ways to update the program counter, including regular updates, jumps, relative jumps, and conditional jumps (jump if not zero).

```go
type PcUpdate uint

const (
	PcUpdateRegular PcUpdate = 0
	PcUpdateJump    PcUpdate = 1
	PcUpdateJumpRel PcUpdate = 2
	PcUpdateJnz     PcUpdate = 3
)
```

The ApUpdate constants represent various ways of updating an address pointer, including regular updates, and different addition types.

```go
type ApUpdate uint

const (
	ApUpdateRegular ApUpdate = 0
	ApUpdateAdd     ApUpdate = 1
	ApUpdateAdd1    ApUpdate = 2
	ApUpdateAdd2    ApUpdate = 3
)
```

The FpUpdate constants define different ways of updating the frame pointer, including regular updates, addition with a specific offset, and destination updates.

```go
type FpUpdate uint

const (
	FpUpdateRegular FpUpdate = 0
	FpUpdateAPPlus2 FpUpdate = 1
	FpUpdateDst     FpUpdate = 2
)
```

The Opcode constants represent different types of operations or instructions, including no operation, assertion checks, function calls, and returns.

```go
type Opcode uint

const (
	NOp      Opcode = 0
	AssertEq Opcode = 1
	Call     Opcode = 2
	Ret      Opcode = 4
)
```

Now, once everything is set up, we only have to retrive each field by getting its representative bits. We do that, creating different bitmasks to just get the value. 

1. Constants and Masks:
    The method starts by defining constants and masks for various fields and properties of the instruction. These constants are used to extract specific bits from the encoded instruction and decode them into meaningful values. For instance, the HighBit constant represents the highest bit (bit 63) of the instruction, and various other masks are defined to extract different fields like destination register, opcode, update types, etc.

    ```go
    func DecodeInstruction(encodedInstruction uint64) (Instruction, error) {
        const HighBit uint64 = 1 << 63
        const DstRegMask uint64 = 0x0001
        const DstRegOff uint64 = 0
        const Op0RegMask uint64 = 0x0002
        const Op0RegOff uint64 = 1
        const Op1SrcMask uint64 = 0x001C
        const Op1SrcOff uint64 = 2
        const ResLogicMask uint64 = 0x0060
        const ResLogicOff uint64 = 5
        const PcUpdateMask uint64 = 0x0380
        const PcUpdateOff uint64 = 7
        const ApUpdateMask uint64 = 0x0C00
        const ApUpdateOff uint64 = 10
        const OpcodeMask uint64 = 0x7000
        const OpcodeOff uint64 = 12
    ```

2. Checking High Bit:
    The first check in the method is whether the highest bit (bit 63) of the encoded instruction is set to zero. If it's not zero, this indicates an error, and the function returns an ErrNonZeroHighBitError.

    ```go
    if encodedInstruction&HighBit != 0 {
		return Instruction{}, ErrNonZeroHighBitError
	}
    ```

3. Extracting Offsets:
    The method extracts three offsets from the encoded instruction. These offsets represent memory addresses used in the instruction. They are extracted using bitwise operations and masks to get the lower 16 bits of three different sections of the instruction.

    ```go
    var offset0 = fromBiasedRepresentation((encodedInstruction) & 0xFFFF)
    var offset1 = fromBiasedRepresentation((encodedInstruction >> 16) & 0xFFFF)
    var offset2 = fromBiasedRepresentation((encodedInstruction >> 32) & 0xFFFF)

    -----------------------------------------------------------------------------------------
    func fromBiasedRepresentation(offset uint64) int {
        var bias uint16 = 1 << 15
        return int(int16(uint16(offset) - bias))    
    }
    ```

4. Extracting Flags:
    The next step is to extract the flag section of the encoded instruction, which holds information about registers, sources, updates, and opcodes. This flag section is extracted using a bit shift to the right by 48 positions (which discards the lower 48 bits).
    
    ```go
    var flags = encodedInstruction >> 48
    ```

5. Decoding Fields:
    Using the extracted flag section, the method decodes various fields like destination register, op0 register, op1 source, result logic, pc update, ap update, and opcode. These fields are decoded by extracting specific bits from the flag section and mapping them to their corresponding enum values.

    ```go 
	var dstRegNum = (flags & DstRegMask) >> DstRegOff
	var op0RegNum = (flags & Op0RegMask) >> Op0RegOff
	var op1SrcNum = (flags & Op1SrcMask) >> Op1SrcOff
	var resLogicNum = (flags & ResLogicMask) >> ResLogicOff
	var pcUpdateNum = (flags & PcUpdateMask) >> PcUpdateOff
	var apUpdateNum = (flags & ApUpdateMask) >> ApUpdateOff
	var opCodeNum = (flags & OpcodeMask) >> OpcodeOff

	var dstRegister Register
	var op0Register Register
	var op1Src Op1Src
	var pcUpdate PcUpdate
	var res ResLogic
	var opcode Opcode
	var apUpdate ApUpdate
	var fpUpdate FpUpdate

	if dstRegNum == 1 {
		dstRegister = FP
	} else {
		dstRegister = AP
	}

	if op0RegNum == 1 {
		op0Register = FP
	} else {
		op0Register = AP
	}

	switch op1SrcNum {
        case 0:
            op1Src = Op1SrcOp0
        case 1:
            op1Src = Op1SrcImm
        case 2:
            op1Src = Op1SrcFP
        case 4:
            op1Src = Op1SrcAP
        default:
            return Instruction{}, ErrInvalidOp1RegError
	}

	switch pcUpdateNum {
        case 0:
            pcUpdate = PcUpdateRegular
        case 1:
            pcUpdate = PcUpdateJump
        case 2:
            pcUpdate = PcUpdateJumpRel
        case 4:
            pcUpdate = PcUpdateJnz
        default:
            return Instruction{}, ErrInvalidPcUpdateError
	}

	switch resLogicNum {
        case 0:
            if pcUpdate == PcUpdateJnz {
                res = ResUnconstrained
            } else {
                res = ResOp1
            }
        case 1:
            res = ResAdd
        case 2:
            res = ResMul
        default:
            return Instruction{}, ErrInvalidResError
	}

	switch opCodeNum {
        case 0:
            opcode = NOp
        case 1:
            opcode = Call
        case 2:
            opcode = Ret
        case 4:
            opcode = AssertEq
        default:
            return Instruction{}, ErrInvalidOpcodeError
	}

	switch apUpdateNum {
        case 0:
            if opcode == Call {
                apUpdate = ApUpdateAdd2
            } else {
                apUpdate = ApUpdateRegular
            }
        case 1:
            apUpdate = ApUpdateAdd
        case 2:
            apUpdate = ApUpdateAdd1
        default:
            return Instruction{}, ErrInvalidApUpdateError
	}

	switch opcode {
        case Call:
            fpUpdate = FpUpdateAPPlus2
        case Ret:
            fpUpdate = FpUpdateDst
        default:
            fpUpdate = FpUpdateRegular
	}
    ```
    
6. Creating the Instruction and Returning the Result:
    With all the necessary information extracted and decoded, the method constructs an Instruction object by assigning the decoded values to its fields and returns the created Instruction object along with a nil error if the decoding process is successful.

    ```go 
    return Instruction{
		Off0:     offset0,
		Off1:     offset1,
		Off2:     offset2,
		DstReg:   dstRegister,
		Op0Reg:   op0Register,
		Op1Addr:  op1Src,
		ResLogic: res,
		PcUpdate: pcUpdate,
		ApUpdate: apUpdate,
		FpUpdate: fpUpdate,
		Opcode:   opcode,
	}, nil
    ```

7. Error Handling:
    If at any point during the decoding process, an unexpected value is encountered or the input doesn't conform to the expected pattern, the method returns an appropriate error. These errors include cases like invalid op1 register, invalid pc update, invalid result logic, invalid opcode, etc.

    ```go
    var ErrNonZeroHighBitError = errors.New("Instruction high bit was not set to zero")
    var ErrInvalidOp1RegError = errors.New("Instruction had invalid Op1 Register")
    var ErrInvalidPcUpdateError = errors.New("Instruction had invalid Pc update")
    var ErrInvalidResError = errors.New("Instruction had an invalid res")
    var ErrInvalidOpcodeError = errors.New("Instruction had an invalid opcode")
    var ErrInvalidApUpdateError = errors.New("Instruction had an invalid Ap Update")
    ```

##### Run instruction

At this point, we have all the information we need from the instruction. Let's run it! 

There are 5 steps to run an instruction, they will be explained in detail later.

    1. Compute the operands of the instruction
    2. Assert the correctness of the operands
    3. Add the context's register state to the trace 
    4. Update registers
    5. Add one to the current step

```go
func (v *VirtualMachine) RunInstruction(instruction *Instruction) error {
	operands, err := v.ComputeOperands(*instruction)
	if err != nil {
		return err
	}

	err = v.OpcodeAssertions(*instruction, operands)
	if err != nil {
		return err
	}

	v.Trace = append(v.Trace, TraceEntry{Pc: v.RunContext.Pc, Ap: v.RunContext.Ap, Fp: v.RunContext.Fp})

	err = v.UpdateRegisters(instruction, &operands)
	if err != nil {
		return err
	}

	v.CurrentStep++
	return nil
}
```

#### Compute operands

Once the instruction has been decoded, it is executed by `RunInstruction` whose first function is to compute operands. This function is in charge of
calculating the addresses of the operands and fetching them from memory. If the function could not fetch the operands then they are deduced from the other operands,
taking in consideration what kind of opcode is being executed. 

```go
func (vm *VirtualMachine) ComputeOperands(instruction Instruction) (Operands, error) {
	var res *memory.MaybeRelocatable

	dst_addr, err := vm.RunContext.ComputeDstAddr(instruction)
	if err != nil {
		return Operands{}, errors.New("FailedToComputeDstAddr")
	}
	dst, _ := vm.Segments.Memory.Get(dst_addr)

	op0_addr, err := vm.RunContext.ComputeOp0Addr(instruction)
	if err != nil {
		return Operands{}, fmt.Errorf("FailedToComputeOp0Addr: %s", err)
	}
	op0, _ := vm.Segments.Memory.Get(op0_addr)

	op1_addr, err := vm.RunContext.ComputeOp1Addr(instruction, op0)
	if err != nil {
		return Operands{}, fmt.Errorf("FailedToComputeOp1Addr: %s", err)
	}
	op1, _ := vm.Segments.Memory.Get(op1_addr)

	if op0 == nil {
		deducedOp0, deducedRes, err := vm.DeduceOp0(&instruction, dst, op1)
		if err != nil {
			return Operands{}, err
		}
		op0 = deducedOp0
		if op0 != nil {
			vm.Segments.Memory.Insert(op0_addr, op0)
		}
		res = deducedRes
	}

	if op1 == nil {
		deducedOp1, deducedRes, err := vm.DeduceOp1(instruction, dst, op0)
		if err != nil {
			return Operands{}, err
		}
		op1 = deducedOp1
		if op1 != nil {
			vm.Segments.Memory.Insert(op1_addr, op1)
		}
		if res == nil {
			res = deducedRes
		}
	}

	if res == nil {
		res, err = vm.ComputeRes(instruction, *op0, *op1)

		if err != nil {
			return Operands{}, err
		}
	}

	if dst == nil {
		deducedDst := vm.DeduceDst(instruction, res)
		dst = deducedDst
		if dst != nil {
			vm.Segments.Memory.Insert(dst_addr, dst)
		}
	}

	operands := Operands{
		Dst: *dst,
		Op0: *op0,
		Op1: *op1,
		Res: res,
	}
	return operands, nil
}
```

##### ComputeDstAddr
The method `ComputeDstAddr` computes the address of the value that will be stored in the Destination (dst) operand. It checks which register its is relative to (wether ap or fp) and gets the direction by adding the instruction's first offset(off0) to the corresponding register.

```go
func (run_context RunContext) ComputeDstAddr(instruction Instruction) (memory.Relocatable, error) {
	var base_addr memory.Relocatable
	switch instruction.DstReg {
	case AP:
		base_addr = run_context.Ap
	case FP:
		base_addr = run_context.Fp
	}

	if instruction.Off0 < 0 {
		return base_addr.SubUint(uint(math.Abs(float64(instruction.Off0))))
	} else {
		return base_addr.AddUint(uint(instruction.Off0))
	}

}
```
##### ComputeOp0Addr

This method is similar to `ComputeDstAddr` but it uses the instruction second offset (off1) to add to the selected register (ap or fp)

```go
func (run_context RunContext) ComputeOp0Addr(instruction Instruction) (memory.Relocatable, error) {
	var base_addr memory.Relocatable
	switch instruction.Op0Reg {
	case AP:
		base_addr = run_context.Ap
	case FP:
		base_addr = run_context.Fp
	}

	if instruction.Off1 < 0 {
		return base_addr.SubUint(uint(math.Abs(float64(instruction.Off1))))
	} else {
		return base_addr.AddUint(uint(instruction.Off1))
	}
}

```

##### ComputeOp1Addr

It computes the address of `Op1` based on the `Op0` operand and the kind of Address the instruction has for `Op1`.
- If its address is `Op1SrcFp` it calculates the direction from Fp register.
- if it is `Op1SrcAp` then if calculates it if from Ap register. 
- If it is an immediate then checks if the offset 2 is 1 and calculates it from the `Pc`. 
- If it is an `Op1SrcOp0` it checks the `Op0` and calculates the direction from it.

Then it performs and addition or a substraction if the `Off2` is negative or positive.

```go
func (run_context RunContext) ComputeOp1Addr(instruction Instruction, op0 *memory.MaybeRelocatable) (memory.Relocatable, error) {
	var base_addr memory.Relocatable

	switch instruction.Op1Addr {
	case Op1SrcFP:
		base_addr = run_context.Fp
	case Op1SrcAP:
		base_addr = run_context.Ap
	case Op1SrcImm:
		if instruction.Off2 == 1 {
			base_addr = run_context.Pc
		} else {
			base_addr = memory.NewRelocatable(0, 0)
			return memory.Relocatable{}, &VirtualMachineError{Msg: "UnknownOp0"}
		}
	case Op1SrcOp0:
		if op0 == nil {
			return memory.Relocatable{}, errors.New("Unknown Op0")
		}
		rel, is_rel := op0.GetRelocatable()
		if is_rel {
			base_addr = rel
		} else {
			return memory.Relocatable{}, errors.New("AddressNotRelocatable")
		}
	}

	if instruction.Off2 < 0 {
		return base_addr.SubUint(uint(math.Abs(float64(instruction.Off2))))
	} else {
		return base_addr.AddUint(uint(instruction.Off2))
	}
}
```

##### DeduceOp0

The method deduces the value of `Op0` if possible (based on `dst` and `Op1`).
If Instruction's opcode is a `Call` `Op0` is deduced by adding the instruction size to the program counter.
- If it is an `AssertEq` then a second switch case is used to check what `ResLogic` is.
- If it is `ResAdd` `Op0` is deduced from the substraction of `Op1` from `Dst`,  if is is `Resmul` the `Op0` is deduced from the division of `Dst` and `Op1` (both felt values). 
- Otherwise op0 is nil.  

The method also deduces `res` by using the value of `dst`

```go
func (vm *VirtualMachine) DeduceOp0(instruction *Instruction, dst *memory.MaybeRelocatable, op1 *memory.MaybeRelocatable) (deduced_op0 *memory.MaybeRelocatable, deduced_res *memory.MaybeRelocatable, error error) {
	switch instruction.Opcode {
	case Call:
		deduced_op0 := vm.RunContext.Pc
		deduced_op0.Offset += instruction.Size()
		return memory.NewMaybeRelocatableRelocatable(deduced_op0), nil, nil
	case AssertEq:
		switch instruction.ResLogic {
		case ResAdd:
			if dst != nil && op1 != nil {
				deduced_op0, err := dst.Sub(*op1)
				if err != nil {
					return nil, nil, err
				}
				return &deduced_op0, dst, nil
			}
		case ResMul:
			if dst != nil && op1 != nil {
				dst_felt, dst_is_felt := dst.GetFelt()
				op1_felt, op1_is_felt := op1.GetFelt()
				if dst_is_felt && op1_is_felt && !op1_felt.IsZero() {
					return memory.NewMaybeRelocatableFelt(dst_felt.Div(op1_felt)), dst, nil

				}
			}
		}
	}
	return nil, nil, nil
}
```

##### DeduceOp1

The method deduces the value of `Op1` if possible (based on `dst` and `Op0`) it also deduces `res` if possible.
- If the instruction opcode is `AssertEq` a switch case is used to check what the `ResLogic` is.
- If it is a `ResOp1` then the value of op1 is equal to the dst operand.
- If `ResLogic` is `ResAdd` op1 is deduced from the substraction of `op0` from `dst`.
- If it is `ResMul` `op1` is deduced from the division of `dst` by `op0`, 

In all the cases `res` is equal to `dst`. if none of the former cases apply then nil is returned.

```go
func (vm *VirtualMachine) DeduceOp1(instruction Instruction, dst *memory.MaybeRelocatable, op0 *memory.MaybeRelocatable) (*memory.MaybeRelocatable, *memory.MaybeRelocatable, error) {
	if instruction.Opcode == AssertEq {
		switch instruction.ResLogic {
		case ResOp1:
			return dst, dst, nil
		case ResAdd:
			if op0 != nil && dst != nil {
				dst_rel, err := dst.Sub(*op0)
				if err != nil {
					return nil, nil, err
				}
				return &dst_rel, dst, nil
			}
		case ResMul:
			dst_felt, dst_is_felt := dst.GetFelt()
			op0_felt, op0_is_felt := op0.GetFelt()
			if dst_is_felt && op0_is_felt && !op0_felt.IsZero() {
				res := memory.NewMaybeRelocatableFelt(dst_felt.Div(op0_felt))
				return res, dst, nil
			}
		}
	}
	return nil, nil, nil
}

```
##### ComputeRes

If the Res value has not been deduced in the previous steps then it is computed based on the `Op0` and `Op1` values.
- If `ResLogic` is `ResOp1` then `res` is equal to `op1`. 
- If it is `ResAdd` then `res` is deduced from the addition of `op0` and `op1`. 
- If it is `ResMul` `res` is deduced from the multiplication of `op0` and `op1`.
- Otherwise `res` is nil.

```go
func (vm *VirtualMachine) ComputeRes(instruction Instruction, op0 memory.MaybeRelocatable, op1 memory.MaybeRelocatable) (*memory.MaybeRelocatable, error) {
	switch instruction.ResLogic {
	case ResOp1:
		return &op1, nil

	case ResAdd:
		maybe_rel, err := op0.Add(op1)
		if err != nil {
			return nil, err
		}
		return &maybe_rel, nil

	case ResMul:
		num_op0, m_type := op0.GetFelt()
		num_op1, other_type := op1.GetFelt()
		if m_type && other_type {
			result := memory.NewMaybeRelocatableFelt(num_op0.Mul(num_op1))
			return result, nil
		} else {
			return nil, errors.New("ComputeResRelocatableMul")
		}

	case ResUnconstrained:
		return nil, nil
	}
	return nil, nil
}
```

##### DeduceDst

If the destination value has not been calculated before then it is deduced based on the Res operand. If the opcode is an `AssertEq` then dst is equal res.
If it is a `Call` then its value is taken from the `Fp` register 

```go
func (vm *VirtualMachine) DeduceDst(instruction Instruction, res *memory.MaybeRelocatable) *memory.MaybeRelocatable {
	switch instruction.Opcode {
	case AssertEq:
		return res
	case Call:
		return memory.NewMaybeRelocatableRelocatable(vm.RunContext.Fp)

	}
	return nil
}
```
#### Opcode assertions

Once we have the instruction's operands to work with, we have to ensure the correctness of them. The first thing we need to differentiate is which type of instruction are we running, we do this by looking at the instruction's opcode.

The posible opcodes we want to perform assertions on are:

  1. AssertEq instruction
  2. Call instruction

In the first option, we need to ensure the result operand is not null (nil in this case) and also that the result operand is equal to the dst operand. If any of those things fail, we throw an error.

On the other hand, the Call instruction, what we do first is define our return pc register, we do that adding the size of the instruction to the current pc. Then, we check our operand op0 is equal to the return pc and our dst operand is the same as the return fp register. If any of those things fail, we throw an error.

If this method returns a nil error, it means operands were computed correctly and we are good to go!

```go
func (vm *VirtualMachine) OpcodeAssertions(instruction Instruction, operands Operands) error {
    switch instruction.Opcode {
    case AssertEq:
        if operands.Res == nil {
            return &VirtualMachineError{"UnconstrainedResAssertEq"}
        }
        if !operands.Res.IsEqual(&operands.Dst) {
            return &VirtualMachineError{"DiffAssertValues"}
        }
    case Call:
        new_rel, err := vm.RunContext.Pc.AddUint(instruction.Size())
        if err != nil {
            return err
        }
        returnPC := memory.NewMaybeRelocatableRelocatable(new_rel)

        if !operands.Op0.IsEqual(returnPC) {
            return &VirtualMachineError{"CantWriteReturnPc"}
        }

        returnFP := vm.RunContext.Fp
        dstRelocatable, _ := operands.Dst.GetRelocatable()
        if !returnFP.IsEqual(&dstRelocatable) {
            return &VirtualMachineError{"CantWriteReturnFp"}
        }
    }

    return nil
}
```

#### Updating Registers

After we succesfully computed the value of the operands, it's now time to update the value of the registers, we will update each register according to the `PcUpdate`, `ApUpdate` and `FpUpdate` fields of the instruction respectively.

##### UpdatePc

As we already know, the pc (program counter) points to the next instruction in memory. When no jumps take place, the pc is updated to point to the next instruction by adding the instruction size to it. The instruction size is 1 if there is no immediate value, and 2 if there is an immediate value following the instruction.
Cairo also supports 3 different types of jumps. The first one is a regular jump, in which the pc takes the value of the res operand. The next one is a relative jump, in which the pc advances by a number of positions set by the res operand. And the last one is a jump not zero, which performs a relative jump, advancing the number of positions given by op1, if the value of the dst operand is not zero, or performs a regular update if the value of the dst operand is zero. The operand will only be zero if it is a Felt value which is zero, relocatable values are never zero.

```go
// Updates the value of PC according to the executed instruction
func (vm *VirtualMachine) UpdatePc(instruction *Instruction, operands *Operands) error {
    switch instruction.PcUpdate {
    case PcUpdateRegular:
        vm.RunContext.Pc.Offset += instruction.Size()
    case PcUpdateJump:
        if operands.Res == nil {
            return errors.New("Res.UNCONSTRAINED cannot be used with PcUpdate.JUMP")
        }
        res, ok := operands.Res.GetRelocatable()
        if !ok {
            return errors.New("An integer value as Res cannot be used with PcUpdate.JUMP")
        }
        vm.RunContext.Pc = res
    case PcUpdateJumpRel:
        if operands.Res == nil {
            return errors.New("Res.UNCONSTRAINED cannot be used with PcUpdate.JUMP_REL")
        }
        res, ok := operands.Res.GetFelt()
        if !ok {
            return errors.New("A relocatable value as Res cannot be used with PcUpdate.JUMP_REL")
        }
        new_pc, err := vm.RunContext.Pc.AddFelt(res)
        if err != nil {
            return err
        }
        vm.RunContext.Pc = new_pc
    case PcUpdateJnz:
        if operands.Dst.IsZero() {
            vm.RunContext.Pc.Offset += instruction.Size()
        } else {
            new_pc, err := vm.RunContext.Pc.AddMaybeRelocatable(operands.Op1)
            if err != nil {
                return err
            }
            vm.RunContext.Pc = new_pc
        }

    }
    return nil
}
```

Some auxiliary methods were added for this method:

###### Instruction.Size

Returns 1 if the instruction has no immediate value or 2 if it has. We can tell that an instruction has an immediate value if the op1 address is given by the immediate value.

```go
func (i *Instruction) Size() uint {
    if i.Op1Addr == Op1SrcImm {
        return 2
    }
    return 1
}
```

###### MaybeRelocatable.IsZero()

Returns true if the value is a Felt that is zero, returns false otherwise

```go
    func (m *MaybeRelocatable) IsZero() bool {
    felt, is_int := m.GetFelt()
    return is_int && felt.IsZero()
}
```

##### UpdateFp

As we already know, the fp (frame pointer) points to the frame of the current function. It can be updated in 4 different ways. A regular fp update means no changes to the fp register. An ap plus 2 update consists on asigning the value of ap to fp and increasing it's offset by two (note: in the code below we only assign the offset, as fp and ap live on the execution segment and therefore have the same segment index). A dst fp update consists in performing either a direct or relative jump based on the value of the dst operand. If dst is a relocatable, fp will take the value of dst, if dst is a felt, fp's offset will be increased by the amount given by dst

```go
// Updates the value of FP according to the executed instruction
func (vm *VirtualMachine) UpdateFp(instruction *Instruction, operands *Operands) error {
    switch instruction.FpUpdate {
    case FpUpdateAPPlus2:
        vm.RunContext.Fp.Offset = vm.RunContext.Ap.Offset + 2
    case FpUpdateDst:
        rel, ok := operands.Dst.GetRelocatable()
        if ok {
            vm.RunContext.Fp = rel
        } else {
            felt, _ := operands.Dst.GetFelt()
            new_fp, err := vm.RunContext.Fp.AddFelt(felt)
            if err != nil {
                return err
            }
            vm.RunContext.Fp = new_fp
        }
    }
    return nil
}
```

##### UpdateAp

And lastly, the ap register points to the next unsused memory cell and has 4 types of update. A regular ap update means no changes to the ap register. An add update consists on advancing the ap register by the amount given by res. And the add1 and add2 updates consist on advancing the op register by 1 and 2 respectively.

```go
// Updates the value of AP according to the executed instruction
func (vm *VirtualMachine) UpdateAp(instruction *Instruction, operands *Operands) error {
    switch instruction.ApUpdate {
    case ApUpdateAdd:
        if operands.Res == nil {
            return errors.New("Res.UNCONSTRAINED cannot be used with ApUpdate.ADD")
        }
        new_ap, err := vm.RunContext.Ap.AddMaybeRelocatable(*operands.Res)
        if err != nil {
            return err
        }
        vm.RunContext.Ap = new_ap
    case ApUpdateAdd1:
        vm.RunContext.Ap.Offset += 1
    case ApUpdateAdd2:
        vm.RunContext.Ap.Offset += 2
    }
    return nil
}
```

### CairoRunner

Now that can can execute cairo steps, lets look at the VM's initialization step.
We will begin by creating our `CairoRunner`:

```go
type CairoRunner struct {
    Program       vm.Program
    Vm            vm.VirtualMachine
    ProgramBase   memory.Relocatable
    executionBase memory.Relocatable
    initialPc     memory.Relocatable
    initialAp     memory.Relocatable
    initialFp     memory.Relocatable
    finalPc       memory.Relocatable
    mainOffset    uint
}

func NewCairoRunner(program vm.Program) *CairoRunner {
    mainIdentifier, ok := (*program.Identifiers)["__main__.main"]
 main_offset := uint(0)
 if ok {
  main_offset = uint(mainIdentifier.PC)
 }
    return &CairoRunner{Program: program, Vm: *vm.NewVirtualMachine(), mainOffset: main_offset}

}
```

Now we will create our `Initialize` method step by step:

```go
// Performs the initialization step, returns the end pointer (pc upon which execution should stop)
func (r *CairoRunner) Initialize() (memory.Relocatable, error) {
    r.initializeSegments()
    end, err := r.initializeMainEntrypoint()
    r.initializeVM()
    return end, err
}
```

#### InitializeSegments

This method will create our program and execution segments

```go
func (r *CairoRunner) initializeSegments() {
    // Program Segment
    r.ProgramBase = r.Vm.Segments.AddSegment()
    // Execution Segment
    r.executionBase = r.Vm.Segments.AddSegment()
}
```

#### initializeMainEntrypoint

This method will initialize the memory and initial register values to begin execution from the main entrypoint, and return the final pc

```go
func (r *CairoRunner) initializeMainEntrypoint() (memory.Relocatable, error) {
    stack := make([]memory.MaybeRelocatable, 0, 2)
    return_fp := r.Vm.Segments.AddSegment()
    return r.initializeFunctionEntrypoint(r.mainOffset, &stack, return_fp)
}
```

#### initializeFunctionEntrypoint

This method will initialize the memory and initial register values to execute a cairo function given its offset within the program segment (aka entrypoint) and return the final pc. In our case, this function will be the main entrypoint, but later on we will be able to use this method to run starknet contract entrypoints.
The stack will then be loaded into the execution segment in the next method. For now, the stack will be empty, but later on it will contain the builtin bases (which are the arguments for the main function), and the function arguments when running a function from a starknet contract.

```go
func (r *CairoRunner) initializeFunctionEntrypoint(entrypoint uint, stack *[]memory.MaybeRelocatable, return_fp memory.Relocatable) (memory.Relocatable, error) {
    end := r.Vm.Segments.AddSegment()
    *stack = append(*stack, *memory.NewMaybeRelocatableRelocatable(end), *memory.NewMaybeRelocatableRelocatable(return_fp))
    r.initialFp = r.executionBase
    r.initialFp.Offset += uint(len(*stack))
    r.initialAp = r.initialFp
    r.finalPc = end
    return end, r.initializeState(entrypoint, stack)
}
```

#### InitializeState

This method will be in charge of loading the program data into the program segment and the stack into the execution segment

```go
func (r *CairoRunner) initializeState(entrypoint uint, stack *[]memory.MaybeRelocatable) error {
    r.initialPc = r.ProgramBase
    r.initialPc.Offset += entrypoint
    // Load program data
    _, err := r.Vm.Segments.LoadData(r.ProgramBase, &r.Program.Data)
    if err == nil {
        _, err = r.Vm.Segments.LoadData(r.executionBase, stack)
    }
    return err
}
```

#### initializeVm

This method will set the values of the VM's `RunContext` with our `CairoRunner`'s initial values

```go
func (r *CairoRunner) initializeVM() {
    r.Vm.RunContext.Ap = r.initialAp
    r.Vm.RunContext.Fp = r.initialFp
    r.Vm.RunContext.Pc = r.initialPc
}
```

With `CairoRunner.Initialize()` now complete we can move on to the execution step:

#### RunUntilPc

This method will continuously execute cairo steps until the end pc, returned by 'CairoRunner.Initialize()' is reached

```go
func (r *CairoRunner) RunUntilPC(end memory.Relocatable) error {
 for r.Vm.RunContext.Pc != end {
  err := r.Vm.Step()
  if err != nil {
   return err
  }
 }
 return nil
```

#### Memory Relocation - function

TODO

#### Builtins

Now that we are able to run a basic fibonacci program, lets step up our game by adding builtins to our VM. A builtin is a low level optimization integrated into the core loop of the VM that allows otherwise expensive computation to be performed more efficiently. Builtins have two ways to operate: via validation rules and via auto-deduction rules. Validation rules are applied to every element that is inserted into a builtin's segment. For example, if I want to verify an ecdsa signature, I can insert it into the ecdsa builtin's segment and let a validation rule take care of verifying the signature. Auto-deduction rules take over during instruction execution, when we can't compute the value of an operand who's address belongs to a builtin segment, we can use that builtin's auto-deduction rule to calculate the value of the operand. For example, If I want to calculate the pedersen hash of two values, I can write the values into the pedersen builtin's segment and then ask for the next memory cell, without builtins, this instruction would have failed, as there is no value stored in that cell, but now we can use auto-deduction rules to calculate the hash and fill in that memory cell.

We will define a basic interface to generalize all of our builtin's behaviour:

```go
type BuiltinRunner interface {
    // Returns the first address of the builtin's memory segment
    Base() memory.Relocatable
    // Returns the name of the builtin
    Name() string
    // Creates a memory segment for the builtin and initializes its base
    InitializeSegments(*memory.MemorySegmentManager)
    // Returns the builtin's initial stack
    InitialStack() []memory.MaybeRelocatable
    // Attempts to deduce the value of a memory cell given by its address. Can return either a nil pointer and an error, if an error arises during the deduction,
    // a valid pointer and nil if the deduction was succesful, or a nil pointer and nil if there is no deduction for the memory cell
    DeduceMemoryCell(memory.Relocatable, *memory.Memory) (*memory.MaybeRelocatable, error)
    // Adds a validation rule to the memory
    // Validation rules are applied when a value is inserted into the builtin's segment
    AddValidationRule(*memory.Memory)
}
```

And now lets integrate this into our existing codebase:

First we will make some modifications to our basic structures:

We will add our builtin runners to the VM:

```go
type VirtualMachine struct {
    RunContext     RunContext
    currentStep    uint
    Segments       memory.MemorySegmentManager
    BuiltinRunners []builtins.BuiltinRunner
}
```

Then we will create two new types to handle validation rules in the `Memory`:

##### ValidationRule

This will represent our builtin's validation rules, they take a memory address and a referenece to the memory, and return a list of validated addresses, for most builtins, this list will contain the address it received if the validation was succesful, but some builtins may return additional addresses.

```go
// A function that validates a memory address and returns a list of validated addresses
type ValidationRule func(*Memory, Relocatable) ([]Relocatable, error)
```

##### AddressSet

As go doesn't have a set type, we created our own really basic set for `Relocatable`s. This will hold the values returned by the validation rules, so that we don't have to run them more than once for each memory cell.

```go
// A Set to store Relocatable values
type AddressSet map[Relocatable]bool

func NewAddressSet() AddressSet {
    return make(map[Relocatable]bool)
}

func (set AddressSet) Add(element Relocatable) {
    set[element] = true
}

func (set AddressSet) Contains(element Relocatable) bool {
    return set[element]
}
```

And we will add them to our `Memory` stuct:

``` go
type Memory struct {
    data                map[Relocatable]MaybeRelocatable
    num_segments        uint
    validation_rules    map[uint]ValidationRule
    validated_addresses AddressSet
}
```

Now we only need to add a way to create this validation rules:

```go
// Adds a validation rule for a given segment
func (m *Memory) AddValidationRule(segment_index uint, rule ValidationRule) {
    m.validation_rules[segment_index] = rule
}
```

And a method that runs validations on a memory address:

```go
// Applies the validation rule for the addr's segment if any
// Skips validation if the address is temporary or if it has been previously validated
func (m *Memory) validateAddress(addr Relocatable) error {
    if addr.SegmentIndex < 0 || m.validated_addresses.Contains(addr) {
        return nil
    }
    rule, ok := m.validation_rules[uint(addr.SegmentIndex)]
    if !ok {
        return nil
    }
    validated_addresses, error := rule(m, addr)
    if error != nil {
        return error
    }
    for _, validated_address := range validated_addresses {
        m.validated_addresses.Add(validated_address)
    }
    return nil
}
```

And we are all set to integrate this new logic into our `Memory`'s `Insert` operation:

```go
// Inserts a value in some memory address, given by a Relocatable value.
func (m *Memory) Insert(addr Relocatable, val *MaybeRelocatable) error {
    // Check that insertions are preformed within the memory bounds
    if addr.SegmentIndex >= int(m.num_segments) {
        return errors.New("Error: Inserting into a non allocated segment")
    }
    // Check for possible overwrites
    prev_elem, ok := m.data[addr]
    if ok && prev_elem != *val {
        return errors.New("Memory is write-once, cannot overwrite memory value")
    }

    m.data[addr] = *val

    return m.validateAddress(addr)
}
```

Now we will initialize the builtins from our `CairoRunner`:

##### NewCairoRunner (Builtins)

Here we will have to iterate over the `Builtins` field of the `Program`, and add the corresponding builtin to the `VirtualMachine`'s `BuiltinRunner` field. We don't have any builtins yet, so we wil add a comment as placeholder and just leave a default case. As we implement more builtins, we will add a case for each of them.

```go
func NewCairoRunner(program vm.Program) (*CairoRunner, error) {
    mainIdentifier, ok := (*program.Identifiers)["__main__.main"]
 main_offset := uint(0)
 if ok {
  main_offset = uint(mainIdentifier.PC)
 }
    runner := CairoRunner{Program: program, Vm: *vm.NewVirtualMachine(), mainOffset: main_offset}
    for _, builtin_name := range program.Builtins {
        switch builtin_name {
        // Add a case for each builtin here, example:
        // case "range_check":
        //     runner.Vm.BuiltinRunners = append(runner.Vm.BuiltinRunners, RangeCheckBuiltin{})
        default:
            return nil, errors.New("Invalid builtin")
        }
    }
    return &runner, nil
}
```

##### InitializeSegments (Builtins)

Here we will also initialize the builtin segments by calling each builtin's `InitializeSegments` method

```go
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
```

##### InitializeMainEntryPoint(Builtins)

Here we will add the builtin's initial_stack to our stack. The builtin's initial_stack is generally made up of the builtin's base, and is what allows the main function to write into the builtin's segment.

```go
func (r *CairoRunner) initializeMainEntrypoint() (memory.Relocatable, error) {
    // When running from main entrypoint, only up to 11 values will be written (9 builtin bases + end + return_fp)
    stack := make([]memory.MaybeRelocatable, 0, 11)
    // Append builtins initial stack to stack
    for i := range r.Vm.BuiltinRunners {
        for _, val := range r.Vm.BuiltinRunners[i].InitialStack() {
            stack = append(stack, val)
        }
    }
    return_fp := r.Vm.Segments.AddSegment()
    return r.initializeFunctionEntrypoint(r.mainOffset, &stack, return_fp)
}
```

##### initializeVm (Builtins)

Here we will add our builtin's validation rules to the `Memory` and use them to validate the meory cells we loaded before

```go
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
```

For this we will add the method `Memory.ValidateExistingMemory`:

```go
func (m *Memory) ValidateExistingMemory() error {
    for addr := range m.data {
        err := m.validateAddress(addr)
        if err != nil {
            return err
        }
    }
    return nil
}
```

Now we will dive deeper into how `auto-deduction` rules come into play during execution:

Before builtins, the basic flow for computing the value of an operand was to first compute its address, and then if we couldn't find it in memory, we would deduce its value based on the other operands.
With the introduction of builtins and their auto-deduction rules, this flow changes a bit. Now we compute the address, use it to fetch the value from memory, if we can't find it in memory we try to use the builtin's auto deduction rules, and if we can't deduce it via builtins we will then deduce it based on the other operands's.
But what does it mean to use the builtin's auto deduction rules to deduce the value of an operand?

##### DeduceMemoryCell

This method will iterate over the builtin runners and try to find a builtin who's base's segment index matches the operand to be deduced's address. That is to say, it checks if the address belongs to a builtin's segment. If a match is found, it uses the builtin's `DeduceMemoryCell` method to run the builtin's auto-deduction rules and calculate the value of the operand

```go
// Applies the corresponding builtin's deduction rules if addr's segment index corresponds to a builtin segment
// Returns nil if there is no deduction for the address
func (vm *VirtualMachine) DeduceMemoryCell(addr memory.Relocatable) (*memory.MaybeRelocatable, error) {
 for i := range vm.BuiltinRunners {
  if vm.BuiltinRunners[i].Base().SegmentIndex == addr.SegmentIndex {
   return vm.BuiltinRunners[i].DeduceMemoryCell(addr, &vm.Segments.Memory)
  }
 }
 return nil, nil
}
```

Now we have to integrate this new method into our `VirtualMachine.ComputeOperands` method:

We will add two helper methods to make our code easier to follow with these new additions:
Both of these methods will be ran fetching either op1 or op0 respectively yields a nil value, and will try to deduce them using both builtins and normal deductions, returning an error if both of these attempts fail

##### ComputeOp0Deductions

```go
// Runs deductions for Op0, first runs builtin deductions, if this fails, attempts to deduce it based on dst and op1
// Also returns res if it was also deduced in the process
// Inserts the deduced operand
// Fails if Op0 was not deduced or if an error arised in the process
func (vm *VirtualMachine) ComputeOp0Deductions(op0_addr memory.Relocatable, instruction *Instruction, dst *memory.MaybeRelocatable, op1 *memory.MaybeRelocatable) (deduced_op0 memory.MaybeRelocatable, deduced_res *memory.MaybeRelocatable, err error) {
 op0, err := vm.DeduceMemoryCell(op0_addr)
 if err != nil {
  return *memory.NewMaybeRelocatableFelt(lambdaworks.FeltZero()), nil, err
 }
 if op0 == nil {
  op0, deduced_res, err = vm.DeduceOp0(instruction, dst, op1)
  if err != nil {
   return *memory.NewMaybeRelocatableFelt(lambdaworks.FeltZero()), nil, err
  }
 }
 if op0 != nil {
  vm.Segments.Memory.Insert(op0_addr, op0)
 } else {
  return *memory.NewMaybeRelocatableFelt(lambdaworks.FeltZero()), nil, errors.New("Failed to compute or deduce op0")
 }
 return *op0, deduced_res, nil
}
```

##### ComputeOp1Deductions

```go
// Runs deductions for Op1, first runs builtin deductions, if this fails, attempts to deduce it based on dst and op0
// Also updates res if it was also deduced in the process
// Inserts the deduced operand
// Fails if Op1 was not deduced or if an error arised in the process
func (vm *VirtualMachine) ComputeOp1Deductions(op1_addr memory.Relocatable, instruction *Instruction, dst *memory.MaybeRelocatable, op0 *memory.MaybeRelocatable, res *memory.MaybeRelocatable) (memory.MaybeRelocatable, error) {
 op1, err := vm.DeduceMemoryCell(op1_addr)
 if err != nil {
  return *memory.NewMaybeRelocatableFelt(lambdaworks.FeltZero()), err
 }
 if op1 == nil {
  var deducedRes *memory.MaybeRelocatable
  op1, deducedRes, err = vm.DeduceOp1(instruction, dst, op0)
  if err != nil {
   return *memory.NewMaybeRelocatableFelt(lambdaworks.FeltZero()), err
  }
  if res == nil {
   res = deducedRes
  }
 }
 if op1 != nil {
  vm.Segments.Memory.Insert(op1_addr, op1)
 } else {
  return *memory.NewMaybeRelocatableFelt(lambdaworks.FeltZero()), errors.New("Failed to compute or deduce op1")
 }
 return *op1, nil
}
```

Now we integrate these two new methods into our previous `ComputeOperands` method:

```go
func (vm *VirtualMachine) ComputeOperands(instruction Instruction) (Operands, error) {
 var res *memory.MaybeRelocatable

 dst_addr, err := vm.RunContext.ComputeDstAddr(instruction)
 if err != nil {
  return Operands{}, errors.New("FailedToComputeDstAddr")
 }
 dst, _ := vm.Segments.Memory.Get(dst_addr)

 op0_addr, err := vm.RunContext.ComputeOp0Addr(instruction)
 if err != nil {
  return Operands{}, fmt.Errorf("FailedToComputeOp0Addr: %s", err)
 }
 op0_op, _ := vm.Segments.Memory.Get(op0_addr)

 op1_addr, err := vm.RunContext.ComputeOp1Addr(instruction, op0_op)
 if err != nil {
  return Operands{}, fmt.Errorf("FailedToComputeOp1Addr: %s", err)
 }
 op1_op, _ := vm.Segments.Memory.Get(op1_addr)

  var op0 memory.MaybeRelocatable
 if op0_op != nil {
  op0 = *op0_op
 } else {
  op0, res, err = vm.ComputeOp0Deductions(op0_addr, &instruction, dst, op1_op)
  if err != nil {
   return Operands{}, err
  }
 }

 var op1 memory.MaybeRelocatable
 if op1_op != nil {
  op1 = *op1_op
 } else {
  op1, err = vm.ComputeOp1Deductions(op1_addr, &instruction, dst, op0_op, res)
  if err != nil {
   return Operands{}, err
  }
 }
    if res == nil {
  res, err = vm.ComputeRes(instruction, op0, op1)

  if err != nil {
   return Operands{}, err
  }
 }

 if dst == nil {
  deducedDst := vm.DeduceDst(instruction, res)
  dst = deducedDst
  if dst != nil {
   vm.Segments.Memory.Insert(dst_addr, dst)
  }
 }

 operands := Operands{
  Dst: *dst,
  Op0: op0,
  Op1: op1,
  Res: res,
 }
 return operands, nil
}
```

With all of our builtin logic integrated into the codebase, we can implement any builtin and use it in our cairo programs while worrying only about implementing the `BuiltinRunner` interface and creating the builtin in the `NewCairoRunner` function.

##### RangeCheck

The `RangeCheck` builtin does a very simple thing: it asserts that a given number is in the range $[0, 2^{128})$, i.e., that it's greater than zero and less than $2^{128}$. This might seem superficial but it is used for a lot of different things in Cairo, including comparing numbers. Whenever a program asserts that some number is less than other, the range check builtin is being called underneath. 

TODO: explain this better, it's not entirely clear why $2^{128}$ was chosen.

Let's now talk about how to implement the `RangeCheckBuiltinRunner`.

We have getter functions just to obtain information about the builtin. The `Name` method is used when iterating through all the builtins of the program so we can switch to the correct execution. 

```go
func (r *RangeCheckBuiltinRunner) Base() memory.Relocatable {
	return r.base
}

func (r *RangeCheckBuiltinRunner) Name() string {
	return "range_check"
}
```

For the `InitializeSegments` method we just add a segment to the memory and store the first address of the segment in the base attribute.

```go
func (r *RangeCheckBuiltinRunner) InitializeSegments(segments *memory.MemorySegmentManager) {
	r.base = segments.AddSegment()
}
```

Next we have the `InitialStack` method, that just returns a stack with the base address appended. 

```go 
func (r *RangeCheckBuiltinRunner) InitialStack() []memory.MaybeRelocatable {
	if r.included {
		stack := []memory.MaybeRelocatable{*memory.NewMaybeRelocatableRelocatable(r.base)}
		return stack
	}
	return []memory.MaybeRelocatable{}
}
```

In this case, the `DeduceMemoryCell` is not used in this builtin, so we return nothing. 

```go
func (r *RangeCheckBuiltinRunner) DeduceMemoryCell(addr memory.Relocatable, mem *memory.Memory) (*memory.MaybeRelocatable, error) {
	return nil, nil
}
```

And finally we have the `AddValidationRule` and the `ValidationRule` methods.

###### AddValidationRule

Receives the memory and adds a new validation rule to it for the builtin segment. 

###### ValidationRule

Receives the memory and an address and it checks if the value in that address is a `felt` and then if it's inside the range. To do so, it checks that the necessary number of bits for representing the felt is not greater than the bits for representing the upper bound of the range. If it fits in this range, it returns an `Relocatable` array with the address appended. Otherwise returns error. 

```go 
func ValidationRule(mem *memory.Memory, address memory.Relocatable) ([]memory.Relocatable, error) {
	res_val, err := mem.Get(address)
	if err != nil {
		return nil, err
	}
	felt, is_felt := res_val.GetFelt()
	if !is_felt {
		return nil, errors.New("NotFeltElement")
	}
	if felt.Bits() <= N_PARTS*INNER_RC_BOUND_SHIFT {
		return []memory.Relocatable{address}, nil
	}
	return nil, errors.New("RangeCheckNumOutOfBounds")
}

func (r *RangeCheckBuiltinRunner) AddValidationRule(mem *memory.Memory) {
	mem.AddValidationRule(uint(r.base.SegmentIndex), ValidationRule)
}
``````

##### Output

TODO

#### Poseidon

The poseidon builtin is used to compute the poseidon hash function in an efficient way. The poseidon hash used by the builtin differs from a standard poseidon hash in two ways, it uses different constants (becoming its own stark poseidon hash), and it also uses the internal poseidon permutation function instead of calling a poseidon hash function. The reason for the second one is that it allows the builtin to hash more than one element at a time by permuting the three-element poseidon state.

Due to this difference, the best solution is to use a poseidon implementation built specifically for cairo. In our case we are going to use the poseidon hash in the `starknet-crypto` crate of the [starknet-rs](https://github.com/xJonathanLEI/starknet-rs) repo.
The section below will explain how to create a C wrapper to use this crate from our go code, but you can skip it if you want to use your own version in your native language.

##### Importing the `starknet-crypto`rust crate for our poseidon needs

###### Basic Lib Setup

To set up this we will need the following files:

- A rust project that will hold the rust wrapper for our lib
- A C header file that will use the rust lib as its backend
- A Go file that will call the C header and which our VM's code will intetact with.

Our file tree will look like this:

```text
 starknet_crypto
 ┣ lib
 ┃ ┣ starknet_crypto
 ┃ ┃ ┣ src
 ┃ ┃ ┃ ┗ lib.rs
 ┃ ┃ ┣ Cargo.lock
 ┃ ┃ ┗ Cargo.toml
 ┃ ┗ starknet_crypto.h
 ┣ starknet_crypto.go
```

Our Cargo.toml file will look like this:

```toml
[package]
name = "starknet-crypto"
version = "0.1.0"
edition = "2021"

# See more keys and their definitions at https://doc.rust-lang.org/cargo/reference/manifest.html

[dependencies]
libc = "0.2"
starknet-crypto = { version = "0.5.0"}

[lib]
crate-type = ["cdylib", "staticlib", "lib"]
```

We will import libc in our lib.rs as an external crate by adding:

```rust
extern crate libc;
```

In order to build the lib we will add the following lines to our Makefile's `build` target:

```bash
@cd pkg/starknet_crypto/lib/starknet_crypto && cargo build --release
@cp pkg/starknet_crypto/lib/starknet_crypto/target/release/libstarknet_crypto.a pkg/starknet_crypto/lib
```

And in order to import the lib from go we will add the following to our starknet_crypto.go file:

```go
/*
#cgo LDFLAGS: pkg/starknet_crypto/lib/libstarknet_crypto.a -ldl
#include "lib/starknet_crypto.h"
#include <stdlib.h>
*/
import "C"
```

###### Converting Felt to FieldElement

Now that we have the basic setup the first thing we have to do is to define a conversion between our `Felt` in go, a `felt_t` type in C, and starknet-crypto's `FieldElement` types. We will perform these conversions using the big endian byte representation.

In our C header hile (starknet_crypto.h) we will define the types `byte_t` and `felt_t`:

```c
#include <stdint.h>

typedef uint8_t byte_t;

// A 252 bit prime field element (felt), represented as an array of 32 bytes.
typedef byte_t felt_t[32];
```

And we will interpret this `felt_t` in rust (lib.rs file) as a mutable pointer to the first byte in the felt:

```rust
// C representation of a bit array: a raw pointer to a mutable unsigned 8 bits integer.
type Bytes = *mut u8;
```

With these types defined we can now work on converting the C felt representation to a `FieldElement` in rust. To do so we will implement the following conversion functions:

- `field_element_from_bytes`

    We will convert the C pointer to an array of bytes and use it to create a `FieldElement`

    ```rust
    fn field_element_from_bytes(bytes: Bytes) -> FieldElement {
        let array = unsafe {
            let slice: &mut [u8] = std::slice::from_raw_parts_mut(bytes, 32);
            let array: [u8; 32] = slice.try_into().unwrap();
            array
        };
        FieldElement::from_bytes_be(&array).unwrap()
    }
    ```

- `bytes_from_field_element`

    We will convert the `FieldElement` into bytes and insert each byte into the C mutable pointer

    ```rust
    fn bytes_from_field_element(felt: FieldElement, bytes: Bytes) {
        let byte_array = felt.to_bytes_be();
        for i in 0..32 {
            unsafe {
                *bytes.offset(i) = byte_array[i as usize];
            }
        }
    }
    ```

Now we will implement these same conversions but between `Felt` in go and `felt_t` in C. As we can import C types from Go, we don't have to define a type to represent `felt_t`.

- toC

    We convert the `Felt` to bytes and insert each byte into a `felt_t`

    ```go
    func toC(f lambdaworks.Felt) C.felt_t {
        var result C.felt_t
        for i, byte := range f.ToBeBytes() {
            result[i] = C.byte_t(byte)
        }
        return result
    }
    ```

- fromC

    We iterate the `felt_t` value and cast each byte as a `uint8` to build a byte array which we then use to build our `Felt`

    ```go
    func fromC(result C.felt_t) lambdaworks.Felt {
        var bytes [32]uint8
        for i, byte := range result {
            bytes[i] = uint8(byte)
        }
        return lambdaworks.FeltFromBeBytes(&bytes)
    }
    ```

###### Calling the poseidon permutation function

Now that we have our felt types defined we can move on to wrapping the poseidon permutation function. The `poseidon_permute_comp` from `starknet_crypto` receives a mutable state of three felts as an array. To reduce the complexity of our wrapper we will be receiving three felts in our C function.

We will define the following function in our C header file:

```C
// Computes the poseidon hash permutation over a state of three felts
void poseidon_permute(felt_t, felt_t, felt_t);
```

And we will implement it in the rust lib file, using the types and conversions we implemented earlier:

```rust
use starknet_crypto::{poseidon_permute_comp, FieldElement};

#[no_mangle]
extern "C" fn poseidon_permute(
    first_state_felt: Bytes,
    second_state_felt: Bytes,
    third_state_felt: Bytes,
) {
    // Convert state from C representation to FieldElement
    let mut state_array: [FieldElement; 3] = [
        field_element_from_bytes(first_state_felt),
        field_element_from_bytes(second_state_felt),
        field_element_from_bytes(third_state_felt),
    ];
    // Call poseidon permute comp
    poseidon_permute_comp(&mut state_array);
    // Convert state from FieldElement back to C representation
    bytes_from_field_element(state_array[0], first_state_felt);
    bytes_from_field_element(state_array[1], second_state_felt);
    bytes_from_field_element(state_array[2], third_state_felt);
}
```

And with our lib ready, all that is left is to make a go wrapper with our felt conversion functions that calls the C function:

```go
func PoseidonPermuteComp(poseidon_state *[3]lambdaworks.Felt) {
 state := *poseidon_state
 // Convert args to c representation
 first_state_felt := toC(state[0])
 second_state_felt := toC(state[1])
 third_state_felt := toC(state[2])

 // Compute hash using starknet_crypto C wrapper
 C.poseidon_permute(&first_state_felt[0], &second_state_felt[0], &third_state_felt[0])
 // Convert result to Go representation
 var new_poseidon_state = [3]lambdaworks.Felt{
  fromC(first_state_felt),
  fromC(second_state_felt),
  fromC(third_state_felt),
 }
 // Update poseidon state
 *poseidon_state = new_poseidon_state
}
```

Now that we can call a simple poseidon permutation function we can start implementing our poseidon builtin runner!

##### Implementing the PoseidonBuiltinRunner

We will start by defining our `PoseidonBuiltinRunner` and adding it to our VM when creating a `CairoRunner`:

It will contain it's base and a cache of values that we will use later to optimize our `DeduceMemoryCell` method. The included field indicates if a builtin is used by the program, is used in proof_mode, as all builtins have to be present by default, but for now we will always set the included field to true.

```go
type PoseidonBuiltinRunner struct {
 base     memory.Relocatable
 included bool
 cache    map[memory.Relocatable]lambdaworks.Felt
}

func NewPoseidonBuiltinRunner(included bool) *PoseidonBuiltinRunner {
 return &PoseidonBuiltinRunner{included: included, cache: make(map[memory.Relocatable]lambdaworks.Felt)}
}
```

In order to store it as a `BuiltinRunner` we will have to implement the `BuiltinRunner` interface. Aside from `AddValidationRule` & `DeduceMemoryCell`, most builtins share the same behaviour in their methods, so we can just port them from the builtin runners we implemented before:

```go

const POSEIDON_BUILTIN_NAME = "poseidon"

func (p *PoseidonBuiltinRunner) Base() memory.Relocatable {
 return p.base
}

func (p *PoseidonBuiltinRunner) Name() string {
 return POSEIDON_BUILTIN_NAME
}

func (p *PoseidonBuiltinRunner) InitializeSegments(segments *memory.MemorySegmentManager) {
 p.base = segments.AddSegment()
}

func (p *PoseidonBuiltinRunner) InitialStack() []memory.MaybeRelocatable {
 if p.included {
  return []memory.MaybeRelocatable{*memory.NewMaybeRelocatableRelocatable(p.base)}
 } else {
  return nil
 }
}
```

As the poseidon builtin doesn't have validation rules, the method will be left empty:

```go
func (p *PoseidonBuiltinRunner) AddValidationRule(*memory.Memory) {
}
```

Now lets dive into the poseidon builtin's behaviour!

The poseidon builtin memory is divided into instances of 6 cells, 3 input cells and 3 output cells. This means that whenever we want to deduce the value of an output cell, we will look for the input cells, compute the pedersen permutation over them, and write the permutated values to the output cells. As we only deduce the value of one output cell at a time, we will write the value of the output cells to a cache and use them the next time we have to deduce a memory cell so we avoid computing the poseidon hash more than once over the same input values

We define the following constants to represent a poseidon instance:

```go
const POSEIDON_CELLS_PER_INSTANCE = 6
const POSEIDON_INPUT_CELLS_PER_INSTANCE = 3
```

And we can implement `DeduceMemoryCell`:

This method will first check if the cell is an input cell, if it's an input cell then there is nothing to deduce and it returns nil. Then it will check if there is a cached value for that cell and return it. If there is no cached value it will define the addresses of the first input and output cells, and fetch the values of the input cells. If any of the input cells is missing, or is not a felt value, it returns an error. Once it has the three input cells, it performs the poseidon permutation and inserts the permutated value into each output cell's address in the cache. It then returns the value stored in the cache for the address that the method received.

```go
func (p *PoseidonBuiltinRunner) DeduceMemoryCell(address memory.Relocatable, mem *memory.Memory) (*memory.MaybeRelocatable, error) {
 // Check if its an input cell
 index := address.Offset % POSEIDON_CELLS_PER_INSTANCE
 if index < POSEIDON_INPUT_CELLS_PER_INSTANCE {
  return nil, nil
 }

 value, ok := p.cache[address]
 if ok {
  return memory.NewMaybeRelocatableFelt(value), nil
 }

 input_start_addr, _ := address.SubUint(index)
 output_start_address := input_start_addr.AddUint(POSEIDON_INPUT_CELLS_PER_INSTANCE)

 // Build the initial poseidon state
 var poseidon_state [3]lambdaworks.Felt

 for i := uint(0); i < POSEIDON_INPUT_CELLS_PER_INSTANCE; i++ {
  felt, err := mem.GetFelt(input_start_addr.AddUint(i))
  if err != nil {
   return nil, err
  }
  poseidon_state[i] = felt
 }

 // Run the poseidon permutation
 starknet_crypto.PoseidonPermuteComp(&poseidon_state)

 // Insert the new state into the corresponding output cells in the cache
 for i, elem := range poseidon_state {
  p.cache[output_start_address.AddUint(uint(i))] = elem
 }
 return memory.NewMaybeRelocatableFelt(p.cache[address]), nil
}
```

#### Pedersen

TODO

#### Ecdsa

TODO

#### Keccak

TODO

#### Bitwise

This builtin provides a way to work with the basic bit operations `and`, `or` and `xor`. It implements the basic builtin interface methods:

```go
type BitwiseBuiltinRunner struct {
	base     memory.Relocatable
	included bool
}
```
the getter methods: 

```go
func (b *BitwiseBuiltinRunner) Base() memory.Relocatable {
	return r.base
}

func (b *BitwiseBuiltinRunner) Name() string {
	return "range_check"
}
```

For the `InitializeSegments` we just add a segment to the memory and store in the base attribute, the first adress of the segment. 

```go
func (b *BitwiseBuiltinRunner) InitializeSegments(segments *memory.MemorySegmentManager) {
	r.base = segments.AddSegment()
}
```

we also have `InitialStack` method that returns a stack the base address appended

```go 
func (b *BitwiseBuiltinRunner) InitialStack() []memory.MaybeRelocatable {
	if b.included {
		return []memory.MaybeRelocatable{*memory.NewMaybeRelocatableRelocatable(b.base)}
	} else {
		return []memory.MaybeRelocatable{}
	}
}
```

The method `DeducedMemoryCell` fetches the operands from memory and performs the following operations: 
- If the index is less than the number of input cells then the method returns nil
- After the operands are fetched it is checks that both of them are felts because bitwise can not be performed on relocatable or nil values.
- If the number of bits of any operand is greater than `TOTAL_N_BITS` the method fails because we are out of the field.
- The index is used to know which bitwise operation is going to be performed, if is 2 then `and` is executed, if is 3 then `xor` and if is a 4 then `or` is executed
- Otherwise nil value is returned

```go 
const BITWISE_CELLS_PER_INSTANCE = 5
const BITWISE_TOTAL_N_BITS = 251
const BIWISE_INPUT_CELLS_PER_INSTANCE = 2

func (b *BitwiseBuiltinRunner) DeduceMemoryCell(address memory.Relocatable, mem *memory.Memory) (*memory.MaybeRelocatable, error) {
	index := address.Offset % BITWISE_CELLS_PER_INSTANCE
	if index < BIWISE_INPUT_CELLS_PER_INSTANCE {
		return nil, nil
	}

	x_addr, _ := address.SubUint(index)
	y_addr := x_addr.AddUint(1)

	num_x_felt, err := mem.GetFelt(x_addr)
	if err != nil {
		return nil, nil
	}
	num_y_felt, err := mem.GetFelt(y_addr)
	if err != nil {
		return nil, nil
	}

	if num_x_felt.Bits() > BITWISE_TOTAL_N_BITS {
		return nil, ErrFeltBiggerThanPowerOfTwo(num_x_felt)
	}
	if num_y_felt.Bits() > BITWISE_TOTAL_N_BITS {
		return nil, ErrFeltBiggerThanPowerOfTwo(num_y_felt)
	}

	var res *memory.MaybeRelocatable
	switch index {
	case 2:
		res = memory.NewMaybeRelocatableFelt(num_x_felt.And(num_y_felt))
	case 3:
		res = memory.NewMaybeRelocatableFelt(num_x_felt.Xor(num_y_felt))
	case 4:
		res = memory.NewMaybeRelocatableFelt(num_x_felt.Or(num_y_felt))
	default:
		res = nil
	}
	return res, nil

}
```

Finally `AddValidationRule` is empty in this case

``` go
func (b *BitwiseBuiltinRunner) AddValidationRule(*memory.Memory) {}
```

#### EcOp

TODO

#### SegmentArena

TODO

#### Hints



