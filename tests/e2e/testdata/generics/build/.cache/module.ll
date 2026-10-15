; zyx
target triple = "x86_64-pc-windows-msvc"
%s.0 = type {i64, i64}
%s.1 = type {%s.0, %s.0}
%s.2 = type {{ ptr, i64 }, { ptr, i64 }}
@.str.0 = private unnamed_addr constant [5 x i8] c"hello"
@.str.1 = private unnamed_addr constant [5 x i8] c"world"
@.str.2 = private unnamed_addr constant [5 x i8] c"alpha"
@.str.3 = private unnamed_addr constant [4 x i8] c"beta"
@.str.4 = private unnamed_addr constant [4 x i8] c"solo"
declare void @zyxrt_print_str(ptr, i64)
define void @"std/fmt.Print"({ ptr, i64 } %p0_0) {
entry1:
  %t1 = alloca { ptr, i64 }
  store { ptr, i64 } %p0_0, ptr %t1
  %t2 = load { ptr, i64 }, ptr %t1
  %t3 = extractvalue { ptr, i64 } %t2, 0
  %t4 = extractvalue { ptr, i64 } %t2, 1
  call void @zyxrt_print_str(ptr %t3, i64 %t4)
  br label %ret2
ret2:
  ret void
}
declare void @zyxrt_print_nl()
define void @"std/fmt.Println"({ ptr, i64 } %p0_0) {
entry3:
  %t5 = alloca { ptr, i64 }
  store { ptr, i64 } %p0_0, ptr %t5
  %t6 = load { ptr, i64 }, ptr %t5
  %t7 = extractvalue { ptr, i64 } %t6, 0
  %t8 = extractvalue { ptr, i64 } %t6, 1
  call void @zyxrt_print_str(ptr %t7, i64 %t8)
  call void @zyxrt_print_nl()
  br label %ret4
ret4:
  ret void
}
declare void @zyxrt_print_i64(i64)
define void @"std/fmt.PrintI64"(i64 %p0_0) {
entry5:
  %t9 = alloca i64
  store i64 %p0_0, ptr %t9
  %t10 = load i64, ptr %t9
  call void @zyxrt_print_i64(i64 %t10)
  br label %ret6
ret6:
  ret void
}
declare void @zyxrt_print_u64(i64)
define void @"std/fmt.PrintU64"(i64 %p0_0) {
entry7:
  %t11 = alloca i64
  store i64 %p0_0, ptr %t11
  %t12 = load i64, ptr %t11
  call void @zyxrt_print_u64(i64 %t12)
  br label %ret8
ret8:
  ret void
}
declare void @zyxrt_print_f64(double)
define void @"std/fmt.PrintF64"(double %p0_0) {
entry9:
  %t13 = alloca double
  store double %p0_0, ptr %t13
  %t14 = load double, ptr %t13
  call void @zyxrt_print_f64(double %t14)
  br label %ret10
ret10:
  ret void
}
declare void @zyxrt_print_bool(i8)
define void @"std/fmt.PrintBool"(i8 %p0_0) {
entry11:
  %t15 = alloca i8
  store i8 %p0_0, ptr %t15
  %t16 = load i8, ptr %t15
  call void @zyxrt_print_bool(i8 %t16)
  br label %ret12
ret12:
  ret void
}
declare void @zyxrt_print_char(i32)
define void @"std/fmt.PrintChar"(i32 %p0_0) {
entry13:
  %t17 = alloca i32
  store i32 %p0_0, ptr %t17
  %t18 = load i32, ptr %t17
  call void @zyxrt_print_char(i32 %t18)
  br label %ret14
ret14:
  ret void
}
define void @"std/fmt.PrintNl"() {
entry15:
  call void @zyxrt_print_nl()
  br label %ret16
ret16:
  ret void
}
declare ptr @zyxrt_region_alloc_current(i64, i64)
declare void @llvm.memcpy.p0.p0.i64(ptr, ptr, i64, i1)
define void @"example.com/generics.main"() {
entry17:
  %t22 = alloca ptr
  %t29 = alloca ptr
  %t39 = alloca ptr
  %t55 = alloca ptr
  %t19 = call ptr @zyxrt_region_alloc_current(i64 16, i64 8)
  %t20 = getelementptr inbounds %s.0, ptr %t19, i32 0, i32 0
  store i64 3, ptr %t20
  %t21 = getelementptr inbounds %s.0, ptr %t19, i32 0, i32 1
  store i64 4, ptr %t21
  store ptr %t19, ptr %t22
  %t23 = load ptr, ptr %t22
  %t24 = call i64 @"example.com/generics.Pair$$1e9275fc575d5840.First"(ptr %t23)
  call void @"std/fmt.PrintI64"(i64 %t24)
  call void @"std/fmt.PrintNl"()
  %t25 = load ptr, ptr %t22
  %t26 = call i64 @"example.com/generics.Pair$$1e9275fc575d5840.Second"(ptr %t25)
  call void @"std/fmt.PrintI64"(i64 %t26)
  call void @"std/fmt.PrintNl"()
  %t27 = load ptr, ptr %t22
  %t28 = call ptr @"example.com/generics.Pair$$1e9275fc575d5840.Swap"(ptr %t27)
  store ptr %t28, ptr %t29
  %t30 = load ptr, ptr %t29
  %t31 = call i64 @"example.com/generics.Pair$$1e9275fc575d5840.First"(ptr %t30)
  call void @"std/fmt.PrintI64"(i64 %t31)
  call void @"std/fmt.PrintNl"()
  %t32 = load ptr, ptr %t29
  %t33 = call i64 @"example.com/generics.Pair$$1e9275fc575d5840.Second"(ptr %t32)
  call void @"std/fmt.PrintI64"(i64 %t33)
  call void @"std/fmt.PrintNl"()
  %t34 = call ptr @zyxrt_region_alloc_current(i64 32, i64 8)
  %t35 = getelementptr inbounds %s.1, ptr %t34, i32 0, i32 0
  %t36 = load ptr, ptr %t22
  call void @llvm.memcpy.p0.p0.i64(ptr %t35, ptr %t36, i64 16, i1 false)
  %t37 = getelementptr inbounds %s.1, ptr %t34, i32 0, i32 1
  %t38 = load ptr, ptr %t29
  call void @llvm.memcpy.p0.p0.i64(ptr %t37, ptr %t38, i64 16, i1 false)
  store ptr %t34, ptr %t39
  %t40 = load ptr, ptr %t39
  %t41 = call ptr @"example.com/generics.Pair$$218cdf0e9cb07127.First"(ptr %t40)
  %t42 = call i64 @"example.com/generics.Pair$$1e9275fc575d5840.First"(ptr %t41)
  call void @"std/fmt.PrintI64"(i64 %t42)
  call void @"std/fmt.PrintNl"()
  %t43 = load ptr, ptr %t39
  %t44 = call ptr @"example.com/generics.Pair$$218cdf0e9cb07127.Second"(ptr %t43)
  %t45 = call i64 @"example.com/generics.Pair$$1e9275fc575d5840.Second"(ptr %t44)
  call void @"std/fmt.PrintI64"(i64 %t45)
  call void @"std/fmt.PrintNl"()
  %t46 = call ptr @zyxrt_region_alloc_current(i64 32, i64 8)
  %t47 = getelementptr inbounds %s.2, ptr %t46, i32 0, i32 0
  %t48 = getelementptr inbounds [5 x i8], ptr @.str.0, i64 0, i64 0
  %t49 = insertvalue { ptr, i64 } undef, ptr %t48, 0
  %t50 = insertvalue { ptr, i64 } %t49, i64 5, 1
  store { ptr, i64 } %t50, ptr %t47
  %t51 = getelementptr inbounds %s.2, ptr %t46, i32 0, i32 1
  %t52 = getelementptr inbounds [5 x i8], ptr @.str.1, i64 0, i64 0
  %t53 = insertvalue { ptr, i64 } undef, ptr %t52, 0
  %t54 = insertvalue { ptr, i64 } %t53, i64 5, 1
  store { ptr, i64 } %t54, ptr %t51
  store ptr %t46, ptr %t55
  %t56 = load ptr, ptr %t55
  %t57 = call { ptr, i64 } @"example.com/generics.Pair$$330aaedbbea90d8f.First"(ptr %t56)
  call void @"std/fmt.Println"({ ptr, i64 } %t57)
  %t58 = load ptr, ptr %t55
  %t59 = call { ptr, i64 } @"example.com/generics.Pair$$330aaedbbea90d8f.Second"(ptr %t58)
  call void @"std/fmt.Println"({ ptr, i64 } %t59)
  %t60 = call i64 @"example.com/generics.pick$$1e9275fc575d5840"(i64 7, i64 2, i8 1)
  call void @"std/fmt.PrintI64"(i64 %t60)
  call void @"std/fmt.PrintNl"()
  %t61 = getelementptr inbounds [5 x i8], ptr @.str.2, i64 0, i64 0
  %t62 = insertvalue { ptr, i64 } undef, ptr %t61, 0
  %t63 = insertvalue { ptr, i64 } %t62, i64 5, 1
  %t64 = getelementptr inbounds [4 x i8], ptr @.str.3, i64 0, i64 0
  %t65 = insertvalue { ptr, i64 } undef, ptr %t64, 0
  %t66 = insertvalue { ptr, i64 } %t65, i64 4, 1
  %t67 = call { ptr, i64 } @"example.com/generics.pick$$330aaedbbea90d8f"({ ptr, i64 } %t63, { ptr, i64 } %t66, i8 0)
  call void @"std/fmt.Println"({ ptr, i64 } %t67)
  %t68 = call ptr @"example.com/generics.wrap$$1e9275fc575d5840"(i64 9)
  %t69 = call i64 @"example.com/generics.Pair$$1e9275fc575d5840.First"(ptr %t68)
  call void @"std/fmt.PrintI64"(i64 %t69)
  call void @"std/fmt.PrintNl"()
  %t70 = getelementptr inbounds [4 x i8], ptr @.str.4, i64 0, i64 0
  %t71 = insertvalue { ptr, i64 } undef, ptr %t70, 0
  %t72 = insertvalue { ptr, i64 } %t71, i64 4, 1
  %t73 = call ptr @"example.com/generics.wrap$$330aaedbbea90d8f"({ ptr, i64 } %t72)
  %t74 = call { ptr, i64 } @"example.com/generics.Pair$$330aaedbbea90d8f.Second"(ptr %t73)
  call void @"std/fmt.Println"({ ptr, i64 } %t74)
  br label %ret18
ret18:
  ret void
}
define i64 @"example.com/generics.Pair$$1e9275fc575d5840.First"(ptr %p0_0) {
entry19:
  %t75 = alloca ptr
  %t78 = alloca i64
  store ptr %p0_0, ptr %t75
  %t76 = load ptr, ptr %t75
  %t77 = getelementptr inbounds %s.0, ptr %t76, i32 0, i32 0
  %t79 = load i64, ptr %t77
  store i64 %t79, ptr %t78
  br label %ret20
ret20:
  %t80 = load i64, ptr %t78
  ret i64 %t80
}
define i64 @"example.com/generics.Pair$$1e9275fc575d5840.Second"(ptr %p0_0) {
entry21:
  %t81 = alloca ptr
  %t84 = alloca i64
  store ptr %p0_0, ptr %t81
  %t82 = load ptr, ptr %t81
  %t83 = getelementptr inbounds %s.0, ptr %t82, i32 0, i32 1
  %t85 = load i64, ptr %t83
  store i64 %t85, ptr %t84
  br label %ret22
ret22:
  %t86 = load i64, ptr %t84
  ret i64 %t86
}
define ptr @"example.com/generics.Pair$$1e9275fc575d5840.Swap"(ptr %p0_0) {
entry23:
  %t87 = alloca ptr
  %t97 = alloca ptr
  store ptr %p0_0, ptr %t87
  %t88 = call ptr @zyxrt_region_alloc_current(i64 16, i64 8)
  %t89 = getelementptr inbounds %s.0, ptr %t88, i32 0, i32 0
  %t90 = load ptr, ptr %t87
  %t91 = getelementptr inbounds %s.0, ptr %t90, i32 0, i32 1
  %t92 = load i64, ptr %t91
  store i64 %t92, ptr %t89
  %t93 = getelementptr inbounds %s.0, ptr %t88, i32 0, i32 1
  %t94 = load ptr, ptr %t87
  %t95 = getelementptr inbounds %s.0, ptr %t94, i32 0, i32 0
  %t96 = load i64, ptr %t95
  store i64 %t96, ptr %t93
  store ptr %t88, ptr %t97
  br label %ret24
ret24:
  %t98 = load ptr, ptr %t97
  ret ptr %t98
}
define ptr @"example.com/generics.Pair$$218cdf0e9cb07127.First"(ptr %p0_0) {
entry25:
  %t99 = alloca ptr
  %t102 = alloca ptr
  store ptr %p0_0, ptr %t99
  %t100 = load ptr, ptr %t99
  %t101 = getelementptr inbounds %s.1, ptr %t100, i32 0, i32 0
  store ptr %t101, ptr %t102
  br label %ret26
ret26:
  %t103 = load ptr, ptr %t102
  ret ptr %t103
}
define ptr @"example.com/generics.Pair$$218cdf0e9cb07127.Second"(ptr %p0_0) {
entry27:
  %t104 = alloca ptr
  %t107 = alloca ptr
  store ptr %p0_0, ptr %t104
  %t105 = load ptr, ptr %t104
  %t106 = getelementptr inbounds %s.1, ptr %t105, i32 0, i32 1
  store ptr %t106, ptr %t107
  br label %ret28
ret28:
  %t108 = load ptr, ptr %t107
  ret ptr %t108
}
define ptr @"example.com/generics.Pair$$218cdf0e9cb07127.Swap"(ptr %p0_0) {
entry29:
  %t109 = alloca ptr
  %t117 = alloca ptr
  store ptr %p0_0, ptr %t109
  %t110 = call ptr @zyxrt_region_alloc_current(i64 32, i64 8)
  %t111 = getelementptr inbounds %s.1, ptr %t110, i32 0, i32 0
  %t112 = load ptr, ptr %t109
  %t113 = getelementptr inbounds %s.1, ptr %t112, i32 0, i32 1
  call void @llvm.memcpy.p0.p0.i64(ptr %t111, ptr %t113, i64 16, i1 false)
  %t114 = getelementptr inbounds %s.1, ptr %t110, i32 0, i32 1
  %t115 = load ptr, ptr %t109
  %t116 = getelementptr inbounds %s.1, ptr %t115, i32 0, i32 0
  call void @llvm.memcpy.p0.p0.i64(ptr %t114, ptr %t116, i64 16, i1 false)
  store ptr %t110, ptr %t117
  br label %ret30
ret30:
  %t118 = load ptr, ptr %t117
  ret ptr %t118
}
define { ptr, i64 } @"example.com/generics.Pair$$330aaedbbea90d8f.First"(ptr %p0_0) {
entry31:
  %t119 = alloca ptr
  %t122 = alloca { ptr, i64 }
  store ptr %p0_0, ptr %t119
  %t120 = load ptr, ptr %t119
  %t121 = getelementptr inbounds %s.2, ptr %t120, i32 0, i32 0
  %t123 = load { ptr, i64 }, ptr %t121
  store { ptr, i64 } %t123, ptr %t122
  br label %ret32
ret32:
  %t124 = load { ptr, i64 }, ptr %t122
  ret { ptr, i64 } %t124
}
define { ptr, i64 } @"example.com/generics.Pair$$330aaedbbea90d8f.Second"(ptr %p0_0) {
entry33:
  %t125 = alloca ptr
  %t128 = alloca { ptr, i64 }
  store ptr %p0_0, ptr %t125
  %t126 = load ptr, ptr %t125
  %t127 = getelementptr inbounds %s.2, ptr %t126, i32 0, i32 1
  %t129 = load { ptr, i64 }, ptr %t127
  store { ptr, i64 } %t129, ptr %t128
  br label %ret34
ret34:
  %t130 = load { ptr, i64 }, ptr %t128
  ret { ptr, i64 } %t130
}
define ptr @"example.com/generics.Pair$$330aaedbbea90d8f.Swap"(ptr %p0_0) {
entry35:
  %t131 = alloca ptr
  %t141 = alloca ptr
  store ptr %p0_0, ptr %t131
  %t132 = call ptr @zyxrt_region_alloc_current(i64 32, i64 8)
  %t133 = getelementptr inbounds %s.2, ptr %t132, i32 0, i32 0
  %t134 = load ptr, ptr %t131
  %t135 = getelementptr inbounds %s.2, ptr %t134, i32 0, i32 1
  %t136 = load { ptr, i64 }, ptr %t135
  store { ptr, i64 } %t136, ptr %t133
  %t137 = getelementptr inbounds %s.2, ptr %t132, i32 0, i32 1
  %t138 = load ptr, ptr %t131
  %t139 = getelementptr inbounds %s.2, ptr %t138, i32 0, i32 0
  %t140 = load { ptr, i64 }, ptr %t139
  store { ptr, i64 } %t140, ptr %t137
  store ptr %t132, ptr %t141
  br label %ret36
ret36:
  %t142 = load ptr, ptr %t141
  ret ptr %t142
}
define i64 @"example.com/generics.pick$$1e9275fc575d5840"(i64 %p0_0, i64 %p1_0, i8 %p2_0) {
entry37:
  %t143 = alloca i64
  %t144 = alloca i64
  %t145 = alloca i8
  %t148 = alloca i64
  store i64 %p0_0, ptr %t143
  store i64 %p1_0, ptr %t144
  store i8 %p2_0, ptr %t145
  %t146 = load i8, ptr %t145
  %t147 = icmp ne i8 %t146, 0
  br i1 %t147, label %if.then39, label %if.else40
if.then39:
  %t149 = load i64, ptr %t143
  store i64 %t149, ptr %t148
  br label %ret38
if.else40:
  br label %if.end41
if.end41:
  %t150 = load i64, ptr %t144
  store i64 %t150, ptr %t148
  br label %ret38
ret38:
  %t151 = load i64, ptr %t148
  ret i64 %t151
}
define { ptr, i64 } @"example.com/generics.pick$$330aaedbbea90d8f"({ ptr, i64 } %p0_0, { ptr, i64 } %p1_0, i8 %p2_0) {
entry42:
  %t152 = alloca { ptr, i64 }
  %t153 = alloca { ptr, i64 }
  %t154 = alloca i8
  %t157 = alloca { ptr, i64 }
  store { ptr, i64 } %p0_0, ptr %t152
  store { ptr, i64 } %p1_0, ptr %t153
  store i8 %p2_0, ptr %t154
  %t155 = load i8, ptr %t154
  %t156 = icmp ne i8 %t155, 0
  br i1 %t156, label %if.then44, label %if.else45
if.then44:
  %t158 = load { ptr, i64 }, ptr %t152
  store { ptr, i64 } %t158, ptr %t157
  br label %ret43
if.else45:
  br label %if.end46
if.end46:
  %t159 = load { ptr, i64 }, ptr %t153
  store { ptr, i64 } %t159, ptr %t157
  br label %ret43
ret43:
  %t160 = load { ptr, i64 }, ptr %t157
  ret { ptr, i64 } %t160
}
define ptr @"example.com/generics.wrap$$1e9275fc575d5840"(i64 %p0_0) {
entry47:
  %t161 = alloca i64
  %t167 = alloca ptr
  store i64 %p0_0, ptr %t161
  %t162 = call ptr @zyxrt_region_alloc_current(i64 16, i64 8)
  %t163 = getelementptr inbounds %s.0, ptr %t162, i32 0, i32 0
  %t164 = load i64, ptr %t161
  store i64 %t164, ptr %t163
  %t165 = getelementptr inbounds %s.0, ptr %t162, i32 0, i32 1
  %t166 = load i64, ptr %t161
  store i64 %t166, ptr %t165
  store ptr %t162, ptr %t167
  br label %ret48
ret48:
  %t168 = load ptr, ptr %t167
  ret ptr %t168
}
define ptr @"example.com/generics.wrap$$330aaedbbea90d8f"({ ptr, i64 } %p0_0) {
entry49:
  %t169 = alloca { ptr, i64 }
  %t175 = alloca ptr
  store { ptr, i64 } %p0_0, ptr %t169
  %t170 = call ptr @zyxrt_region_alloc_current(i64 32, i64 8)
  %t171 = getelementptr inbounds %s.2, ptr %t170, i32 0, i32 0
  %t172 = load { ptr, i64 }, ptr %t169
  store { ptr, i64 } %t172, ptr %t171
  %t173 = getelementptr inbounds %s.2, ptr %t170, i32 0, i32 1
  %t174 = load { ptr, i64 }, ptr %t169
  store { ptr, i64 } %t174, ptr %t173
  store ptr %t170, ptr %t175
  br label %ret50
ret50:
  %t176 = load ptr, ptr %t175
  ret ptr %t176
}
define internal void @zyx.globals() {
globals51:
  ret void
}
declare i32 @zyxrt_run_main(ptr, ptr)
define internal void @zyx.main.thunk(ptr %arg) {
  call void @zyx.globals()
  call void @"example.com/generics.main"()
  ret void
}
define i32 @main() {
  call i32 @zyxrt_run_main(ptr @zyx.main.thunk, ptr null)
  ret i32 0
}
