/* Coroutine stack switching for x86-64 under the System V ABI (Linux, the
   BSDs, macOS, Android, HarmonyOS).
 *
 * void zyxrt_ctx_switch(void **from, void **to);
 *
 * A context is one word: the stack pointer of a suspended coroutine. The
 * switch saves the callee-saved registers on the current stack, records the
 * stack pointer, loads the other coroutine's stack pointer, restores its
 * registers, and returns into it. There is no system call and no kernel
 * involvement: this is the whole cost of a coroutine handoff.
 *
 * The registers are the ones the ABI makes the callee responsible for:
 * rbx, rbp, r12-r15.
 */

#if defined(__x86_64__) && !defined(_WIN32)

	.text
	.globl	zyxrt_ctx_switch
	.type	zyxrt_ctx_switch, @function
zyxrt_ctx_switch:
	pushq	%rbp
	pushq	%rbx
	pushq	%r12
	pushq	%r13
	pushq	%r14
	pushq	%r15
	movq	%rsp, (%rdi)
	movq	(%rsi), %rsp
	popq	%r15
	popq	%r14
	popq	%r13
	popq	%r12
	popq	%rbx
	popq	%rbp
	ret
	.size	zyxrt_ctx_switch, .-zyxrt_ctx_switch

/* The number of registers the switch saves, which the runtime uses to
   prepare the first stack of a coroutine. */
	.globl	zyxrt_ctx_saved_words
	.type	zyxrt_ctx_saved_words, @object
zyxrt_ctx_saved_words:
	.quad	6

/* The bytes of vector state the switch saves, which the System V ABI makes
   the caller's responsibility rather than the callee's. */
	.globl	zyxrt_ctx_saved_bytes
zyxrt_ctx_saved_bytes:
	.quad	0

#endif
