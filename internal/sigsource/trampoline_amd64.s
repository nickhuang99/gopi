#include "textflag.h"

// tramp is installed as an SA_SIGINFO handler for a watched signal. The kernel
// enters it with the C ABI: DI = signo, SI = *siginfo_t, DX = *ucontext_t.
//
// It copies si_code / si_pid / si_uid into ·captured, then TAIL-JUMPS into the
// handler it displaced (in a gopi process: runtime.sigtramp), so the signal
// still reaches signal.Notify with the disposition it always had.
//
// REGISTER DISCIPLINE. Only CALLER-SAVED registers are touched: R8, R9, R10,
// R11, RAX, RCX. R12–R15 and RBX/RBP are callee-saved, and a NOSPLIT entry that
// clobbered them would corrupt the registers of the goroutine the signal
// interrupted (the runtime restores the interrupted context from the frame the
// kernel built, but the runtime's own sigtramp also reads live callee-saved
// state — so "the kernel saved it" is not a licence to scribble on them).
// DI/SI/DX are the chain's arguments and are never written.
//
// FIELD OFFSETS (x86_64 siginfo_t): si_signo 0, si_errno 4, si_code 8, then the
// union at 16 — for the kill(2)/tgkill(2)/sigqueue(2) members (_kill/_rt) that
// is si_pid at 16 and si_uid at 20. si_uid is at 20, NOT 24: the union
// members with a wider second field (_sigchld, _timer) put something else
// there, and reading 24 read that padding — measured in the prototype that
// produced this file (si_pid correct, si_uid 0 for a uid-1000 sender).
TEXT ·tramp(SB),NOSPLIT|NOFRAME,$0-0
	MOVQ SI, R10                      // R10 = siginfo_t*
	MOVLQZX DI, R9                    // R9 = signo
	MOVQ R9, R11
	SHLQ $4, R11                      // R11 = signo * slotWords(4) * 4 bytes
	LEAQ ·captured(SB), R8
	MOVL 8(R10), R9                   // si_code
	MOVL R9, (R8)(R11*1)
	MOVL 16(R10), R9                  // si_pid
	MOVL R9, 4(R8)(R11*1)
	MOVL 20(R10), R9                  // si_uid
	MOVL R9, 8(R8)(R11*1)
	MOVLQZX DI, R9
	SHLQ $3, R9                       // R9 = signo * 8 (uintptr words)
	LEAQ ·prevHandler(SB), R8
	MOVQ (R8)(R9*1), R10
	MOVQ R10, R9
	JMP R9                            // tail-jump into the handler we displaced
