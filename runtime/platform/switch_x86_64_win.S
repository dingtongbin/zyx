/* Coroutine stack switching for x86-64 under the Windows ABI.
 *
 * void zyxrt_ctx_switch(void *from, void *to);
 *
 * The Windows ABI makes the callee responsible for more registers than the
 * System V one: rbx, rbp, rdi, rsi, r12-r15, and the low halves of the
 * vector registers xmm6-xmm15. Saving them all keeps a coroutine's
 * floating-point work intact across a handoff. There is no system call.
 *
 * The vector state moves with unaligned moves, because the frame the
 * runtime prepares for a coroutine's first entry does not put the vector
 * area at the offset a suspended frame uses. The addresses are aligned
 * either way in practice, so the cost is nil.
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
	movups	%xmm6, 0(%rsp)
	movups	%xmm7, 16(%rsp)
	movups	%xmm8, 32(%rsp)
	movups	%xmm9, 48(%rsp)
	movups	%xmm10, 64(%rsp)
	movups	%xmm11, 80(%rsp)
	movups	%xmm12, 96(%rsp)
	movups	%xmm13, 112(%rsp)
	movups	%xmm14, 128(%rsp)
	movups	%xmm15, 144(%rsp)
	movq	%rsp, (%rcx)
	movq	(%rdx), %rsp
	movups	0(%rsp), %xmm6
	movups	16(%rsp), %xmm7
	movups	32(%rsp), %xmm8
	movups	48(%rsp), %xmm9
	movups	64(%rsp), %xmm10
	movups	80(%rsp), %xmm11
	movups	96(%rsp), %xmm12
	movups	112(%rsp), %xmm13
	movups	128(%rsp), %xmm14
	movups	144(%rsp), %xmm15
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
