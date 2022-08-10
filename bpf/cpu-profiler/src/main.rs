#![no_std]
#![no_main]
#![feature(core_intrinsics)]
#![warn(clippy::all)]

// TODO(kakkoyun): Enable this when needed task struct is needed.
// #[allow(non_upper_case_globals)]
// #[allow(non_snake_case)]
// #[allow(non_camel_case_types)]
// #[allow(dead_code)]
// mod vmlinux;
//
// use vmlinux::task_struct;

use aya_bpf::{bindings::{BPF_F_USER_STACK, BPF_NOEXIST}, macros::{map, perf_event}, maps::{Array, HashMap, StackTrace}, programs::PerfEventContext, BpfContext, PtRegs};

#[no_mangle]
#[link_section = "license"]
static LICENSE: [u8; 4] = *b"GPL\0";

const MAX_STACK_ADDRESSES: u32 = 1024;
const MAX_STACK_DEPTH: u32 = 127;
// Absolute maximum stack size limit is 512 bytes for eBPF programs.
// 512 / 8 byte address = 64.
// Since some stack slots for other variables this needs to be lower.
// TODO(kakkoyun): Use BPF_MAP_TYPE_PER_CPU_ARRAY to circumvent this limit.
// - https://lwn.net/Articles/674443/
const EH_FRAME_MAX_STACK_DEPTH: usize = 48;
const MAX_BIN_SEARCH_DEPTH: u32 = 24;
const EH_FRAME_ENTRIES: u32 = 0xff_ffff;

#[derive(Clone, Copy)]
#[repr(C)]
pub struct Instruction {
    op: u64,
    offset: i64,
}

#[repr(C)]
pub struct StackCountKey {
    pid: u32,
    user_stack_id: i32,
    kernel_stack_id: i32,
}

#[map(name = "counts")]
static mut COUNTS: HashMap<StackCountKey, u64> =
    HashMap::with_max_entries(MAX_STACK_ADDRESSES, 0);

#[map(name = "stack_traces")]
static mut STACK_TRACES: StackTrace = StackTrace::with_max_entries(MAX_STACK_DEPTH, 0);

// TODO(kakkoyun): Figure out map of map usage. Right now only unwinds the stack for a specific pid.
// - or use composite keys.
#[map(name = "pid")]
static mut PID: Array<u32> = Array::with_max_entries(1, 0);
#[map(name = "pc")]
static mut PC: Array<u64> = Array::with_max_entries(EHFRAME_ENTRIES, 0);
#[map(name = "rip")]
static mut RIP: Array<Instruction> = Array::with_max_entries(EHFRAME_ENTRIES, 0);
#[map(name = "rsp")]
static mut RSP: Array<Instruction> = Array::with_max_entries(EHFRAME_ENTRIES, 0);

#[map(name = "eh_frame_stack_traces")]
static mut EH_FRAME_STACK_TRACES: HashMap<[u64; EH_FRAME_MAX_STACK_DEPTH], u32> =
    HashMap::with_max_entries(1024, 0);

#[perf_event]
fn profile_cpu(ctx: PerfEventContext) -> u32 {
    unsafe {
        try_profile_cpu(ctx);
    }

    0
}

#[inline(always)]
unsafe fn try_profile_cpu(ctx: PerfEventContext) {
    if ctx.pid() == 0 {
        return;
    }

    // TODO(kakkoyun): Find the correct maps to use using PID and pass it along.
    if let Some(pid) = PID.get(0) {
        if *pid == ctx.pid() {
            let mut stack = [0; EH_FRAME_MAX_STACK_DEPTH];
            // TODO(kakkoyun): !!
            // let regs = ctx.regs();
            backtrace(regs, &mut stack);
            let mut count = EH_FRAME_STACK_TRACES.get_mut(&stack).unwrap_or_default();
            core::intrinsics::atomic_xadd_acqrel(count, 1);
            EH_FRAME_STACK_TRACES.insert(&stack, &count, BPF_NOEXIST.into());
            return;
        }
    }


    let mut key = StackCountKey {
        pid: ctx.tgid(),
        user_stack_id: 0,
        kernel_stack_id: 0,
    };

    if let Ok(stack_id) = STACK_TRACES.get_stackid(&ctx, BPF_F_USER_STACK.into()) {
        key.user_stack_id = stack_id as i32;
    }

    if let Ok(stack_id) = STACK_TRACES.get_stackid(&ctx, 0) {
        key.kernel_stack_id = stack_id as i32;
    }

    try_update_count(&mut key);
}

#[inline(always)]
unsafe fn try_update_count(key: &mut StackCountKey) {
    let one = 1;
    let count = COUNTS.get_mut(&key);
    match count {
        Some(count) => {
            core::intrinsics::atomic_xadd_acqrel(count, 1);
        }
        None => {
            _ = COUNTS.insert(&key, &one, BPF_NOEXIST.into());
        }
    }
}

unsafe fn backtrace(regs: &PtRegs, stack: &mut [u64; EH_FRAME_MAX_STACK_DEPTH]) {
    let mut rip = regs.rip;
    let mut rsp = regs.rsp;
    for d in 0..MAX_STACK_DEPTH {
        stack[d] = rip;
        if rip == 0 {
            break;
        }
        let i = binary_search(rip);

        let ins = if let Some(ins) = RSP.get_mut(i) {
            ins
        } else {
            break;
        };
        let cfa = if let Some(cfa) = execute_instruction(&ins, rip, rsp, 0) {
            cfa
        } else {
            break;
        };

        let ins = if let Some(ins) = RIP.get_mut(i) {
            ins
        } else {
            break;
        };
        rip = execute_instruction(&ins, rip, rsp, cfa).unwrap_or_default();
        rsp = cfa;
    }
}

unsafe fn binary_search(rip: u64) -> u32 {
    let mut left = 0;
    let mut right = CONFIG.get(0).unwrap_or(1) - 1;
    let mut i = 0;
    for _ in 0..MAX_BIN_SEARCH_DEPTH {
        if left > right {
            break;
        }
        i = (left + right) / 2;
        let pc = PC.get_mut(i).unwrap_or(u64::MAX);
        if pc < rip {
            left = i;
        } else {
            right = i;
        }
    }
    i
}

fn execute_instruction(ins: &Instruction, rip: u64, rsp: u64, cfa: u64) -> Option<u64> {
    match ins.op {
        1 => {
            let unsafe_ptr = (cfa as i64 + ins.offset as i64) as *const core::ffi::c_void;
            let mut res: u64 = 0;
            if unsafe { sys::bpf_probe_read(&mut res as *mut _ as *mut _, 8, unsafe_ptr) } == 0 {
                Some(res)
            } else {
                None
            }
        }
        2 => Some((rip as i64 + ins.offset as i64) as u64),
        3 => Some((rsp as i64 + ins.offset as i64) as u64),
        _ => None,
    }
}

#[panic_handler]
fn panic(_info: &core::panic::PanicInfo) -> ! {
    unsafe { core::hint::unreachable_unchecked() }
}