/* Coroutine stack switching for x86-64 under the Windows ABI.
 *
 * void zyxrt_ctx_switch(void *from, void *to);
 *
 * The Windows ABI makes the callee responsible for more registers than the
 * System V one: rbx, rbp, rdi, rsi, r12-r15, and the low halves of the
 * vector registers xmm6-xmm15. Saving them all keeps a coroutine's
 * floating-point work intact across a handoff. There is no system call.
 */

#if defined(__x86_64__) && defined(_WIN32)

	.text
	.globl	zyxrt_ctx_switch
zyxrt_ctx_switch:
	pushq	%rbp
	pushq	%rbx
	pushq	%rdi
	pushq	%rsi
	pushq	%r12
	pushq	%r13
	pushq	%r14
	pushq	%r15
	subq	$168, %rsp
	movaps	%xmm6, 0(%rsp)
	movaps	%xmm7, 16(%rsp)
	movaps	%xmm8, 32(%rsp)
	movaps	%xmm9, 48(%rsp)
	movaps	%xmm10, 64(%rsp)
	movaps	%xmm11, 80(%rsp)
	movaps	%xmm12, 96(%rsp)
	movaps	%xmm13, 112(%rsp)
	movaps	%xmm14, 128(%rsp)
	movaps	%xmm15, 144(%rsp)
	movq	%rsp, (%rcx)
	movq	(%rdx), %rsp
	movaps	0(%rsp), %xmm6
	movaps	16(%rsp), %xmm7
	movaps	32(%rsp), %xmm8
	movaps	48(%rsp), %xmm9
	movaps	64(%rsp), %xmm10
	movaps	80(%rsp), %xmm11
	movaps	96(%rsp), %xmm12
	movaps	112(%rsp), %xmm13
	movaps	128(%rsp), %xmm14
	movaps	144(%rsp), %xmm15
	addq	$168, %rsp
	popq	%r15
	popq	%r14
	popq	%r13
	popq	%r12
	popq	%rsi
	popq	%rdi
	popq	%rbx
	popq	%rbp
	ret

/* __chkstk is what the compiler calls before a frame larger than a page is
   taken, to walk the pages the frame is about to use. A coroutine's stack is
   not the thread's, so walking pages the runtime has not committed would fault
   where nothing can report it. The whole frame is committed by the check the
   compiler puts in the function's own body, which runs before anything writes
   to that frame, so this has nothing to walk: the call itself descends one
   word, which the margin the check keeps below every frame pays for. */
	.globl	__chkstk
__chkstk:
	ret

/* zyxrt_ctx_enter is where a coroutine's first frame returns to. A frame
   resumed by the switch lands with the alignment of a caller rather than of
   a function, so the trampoline sets the alignment a function expects and
   calls the coroutine entry. Every coroutine then starts exactly the way a
   resumed one continues, and the layout of the first frame carries no
   special case.

   It has an entry in the function table, and no prologue to describe: the
   address the switch returned to it is the null return address written at the top
   of the stack, so a walk that unwinds this frame reads the end of the stack
   rather than a leaf frame whose return address is whatever lies above it. The
   system's own top frame is described the same way, and a walk over a coroutine's
   frames then ends where the stack says it does. */
	.globl	zyxrt_ctx_enter
zyxrt_ctx_enter:
	andq	$-16, %rsp
	call	zyxrt_sched_enter
	ud2

/* The number of registers the switch saves, which the runtime uses to
   prepare the first stack of a coroutine. */
	.globl	zyxrt_ctx_saved_words
zyxrt_ctx_saved_words:
	.quad	8

/* The bytes of vector state the switch saves. */
	.globl	zyxrt_ctx_saved_bytes
zyxrt_ctx_saved_bytes:
	.quad	168

#endif
