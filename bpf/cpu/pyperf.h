// Copyright (c) Facebook, Inc. and its affiliates.
// Licensed under the Apache License, Version 2.0 (the "License")
//
// Copyright 2023 The Parca Authors

#include "basic_types.h"
#include "common.h"

#define PYTHON_STACK_FRAMES_PER_PROG 16
#define PYTHON_STACK_PROG_CNT 5
#define MAX_STACK (PYTHON_STACK_FRAMES_PER_PROG * PYTHON_STACK_PROG_CNT)

#define PYPERF_STACK_WALKING_PROGRAM_IDX 0
// #define PYPERF_THREAD_STATE_PROGRAM_IDX 1

typedef struct {
  // u64 start_time;
  u64 interpreter_addr;
  u64 thread_state_addr;
  u32 py_version;
} ProcessInfo;
// TODO(kakkoyun): Rename to InterpreterInfo to avoid confusion with the cpu.bpf.c's ProcessInfo.

enum python_stack_status {
  STACK_COMPLETE = 0,
  STACK_TRUNCATED = 1,
  STACK_ERROR = 2,
};

typedef struct {
  u64 timestamp;
  u32 cpu;
  u32 pid;
  u32 tid;
  enum python_stack_status stack_status;

  stack_trace_t stack;
} Sample;

typedef struct {
  ProcessInfo process_info;
  // int py_version;
  // void *interpreter;
  void *thread_state;
  // u64 current_thread_id;
  // int thread_state_prog_call_count;
  // u64 base_stack;
  void *frame_ptr; // TODO(kakkoyun): Rename to cfp.
  int stack_walker_prog_call_count;
  Sample sample;
} State;

// Offsets of structures across different Python versions:
// For the most part, these fields are named after their corresponding structures in Python.
// They are depicted as structures with 64-bit offset fields named after the requisite fields in the original structure.
// However, there are some deviations:
// 1. PyString - The offsets target the Python string object structure.
//     - Owing to the varying representation of strings across versions, which depends on encoding and interning,
//     the field names don't match those of a specific structure. Here, `data` is the offset pointing to the string's
//     first character, while `size` indicates the offset to the 32-bit integer that denotes the string's byte length
//     (not the character count).
// 2. PyRuntimeState.interp_main - This aligns with the offset of (_PyRuntimeState, interpreters.main).
// 3. PyThreadState.thread - In certain Python versions, this field is referred to as "thread_id".

typedef struct {
  s64 ob_type;
} PyObject;

typedef struct {
  s64 data;
  s64 size;
} PyString;

typedef struct {
  s64 tp_name;
} PyTypeObject;

typedef struct {
  s64 next;
  s64 interp;
  s64 frame;
  s64 thread_id;
  s64 native_thread_id;

  s64 cframe;
} PyThreadState;

typedef struct {
  // since Python 3.11 this structure holds pointer to target FrameObject.
  s64 current_frame;
} PyCFrame;

typedef struct {
  s64 tstate_head;
} PyInterpreterState;

typedef struct {
  s64 interp_main;
} PyRuntimeState;

typedef struct {
  s64 f_back;
  s64 f_code;
  s64 f_lineno;
  s64 f_localsplus;
} PyFrameObject;

typedef struct {
  s64 co_filename;
  s64 co_name;
  s64 co_varnames;
  s64 co_firstlineno;
} PyCodeObject;

typedef struct {
  s64 ob_item;
} PyTupleObject;

typedef struct {
  u32 major_version;
  u32 minor_version;
  u32 patch_version;

  PyObject py_object;
  PyString py_string;
  PyTypeObject py_type_object;
  PyThreadState py_thread_state;
  PyCFrame py_cframe;
  PyInterpreterState py_interpreter_state;
  PyRuntimeState py_runtime_state;
  PyFrameObject py_frame_object;
  PyCodeObject py_code_object;
  PyTupleObject py_tuple_object;
} PythonVersionOffsets;
