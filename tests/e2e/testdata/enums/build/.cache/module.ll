; zyx
target triple = "x86_64-pc-windows-msvc"
%s.0 = type {i8, {{double}, [8 x i8]}}
%s.1 = type {i8, {{i64}, [8 x i8]}}
@.str.0 = private unnamed_addr constant [4 x i8] c"zero"
@.str.1 = private unnamed_addr constant [3 x i8] c"abc"
@.str.2 = private unnamed_addr constant [3 x i8] c"abd"
declare void @zyxrt_print_str(ptr, i64)
define void @"std/fmt.Print"({ ptr, i64 } %p0_0) uwtable {
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
define void @"std/fmt.Println"({ ptr, i64 } %p0_0) uwtable {
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
define void @"std/fmt.PrintI64"(i64 %p0_0) uwtable {
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
define void @"std/fmt.PrintU64"(i64 %p0_0) uwtable {
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
define void @"std/fmt.PrintF64"(double %p0_0) uwtable {
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
define void @"std/fmt.PrintBool"(i8 %p0_0) uwtable {
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
define void @"std/fmt.PrintChar"(i32 %p0_0) uwtable {
entry13:
  %t17 = alloca i32
  store i32 %p0_0, ptr %t17
  %t18 = load i32, ptr %t17
  call void @zyxrt_print_char(i32 %t18)
  br label %ret14
ret14:
  ret void
}
define void @"std/fmt.PrintNl"() uwtable {
entry15:
  call void @zyxrt_print_nl()
  br label %ret16
ret16:
  ret void
}
define double @"example.com/enums.Shape.Area"(ptr %p0_0) uwtable {
entry17:
  %t19 = alloca ptr
  %t23 = alloca double
  %t29 = alloca double
  %t40 = alloca double
  %t42 = alloca double
  store ptr %p0_0, ptr %t19
  %t20 = load ptr, ptr %t19
  %t21 = load i8, ptr %t20
  %t22 = icmp eq i8 %t21, 0
  br i1 %t22, label %match.body20, label %match.next21
match.body20:
  store double 0.0, ptr %t23
  br label %ret18
match.next21:
  %t24 = load ptr, ptr %t19
  %t25 = load i8, ptr %t24
  %t26 = icmp eq i8 %t25, 1
  %t27 = getelementptr inbounds %s.0, ptr %t24, i32 0, i32 1
  %t28 = getelementptr inbounds {double}, ptr %t27, i32 0, i32 0
  br i1 %t26, label %match.body22, label %match.next23
match.body22:
  %t30 = load double, ptr %t28
  store double %t30, ptr %t29
  %t31 = load double, ptr %t29
  %t32 = load double, ptr %t29
  %t33 = fmul double %t31, %t32
  %t34 = fmul double %t33, 3.0
  store double %t34, ptr %t23
  br label %ret18
match.next23:
  %t35 = load ptr, ptr %t19
  %t36 = load i8, ptr %t35
  %t37 = icmp eq i8 %t36, 2
  %t38 = getelementptr inbounds %s.0, ptr %t35, i32 0, i32 1
  %t39 = getelementptr inbounds {double, double}, ptr %t38, i32 0, i32 0
  %t41 = getelementptr inbounds {double, double}, ptr %t38, i32 0, i32 1
  br i1 %t37, label %match.body24, label %match.next25
match.body24:
  %t43 = load double, ptr %t39
  store double %t43, ptr %t40
  %t44 = load double, ptr %t41
  store double %t44, ptr %t42
  %t45 = load double, ptr %t40
  %t46 = load double, ptr %t42
  %t47 = fmul double %t45, %t46
  store double %t47, ptr %t23
  br label %ret18
match.next25:
  br label %match.end19
match.end19:
  br label %ret18
ret18:
  %t48 = load double, ptr %t23
  ret double %t48
}
define i64 @"example.com/enums.Color.Code"(ptr %p0_0) uwtable {
entry26:
  %t49 = alloca ptr
  %t53 = alloca i64
  store ptr %p0_0, ptr %t49
  %t50 = load ptr, ptr %t49
  %t51 = load i8, ptr %t50
  %t52 = icmp eq i8 %t51, 0
  br i1 %t52, label %match.body29, label %match.next30
match.body29:
  store i64 1, ptr %t53
  br label %ret27
match.next30:
  %t54 = load ptr, ptr %t49
  %t55 = load i8, ptr %t54
  %t56 = icmp eq i8 %t55, 1
  br i1 %t56, label %match.body31, label %match.next32
match.body31:
  store i64 2, ptr %t53
  br label %ret27
match.next32:
  %t57 = load ptr, ptr %t49
  %t58 = load i8, ptr %t57
  %t59 = icmp eq i8 %t58, 2
  br i1 %t59, label %match.body33, label %match.next34
match.body33:
  store i64 3, ptr %t53
  br label %ret27
match.next34:
  br label %match.end28
match.end28:
  br label %ret27
ret27:
  %t60 = load i64, ptr %t53
  ret i64 %t60
}
define i64 @"example.com/enums.classify"(i64 %p0_0) uwtable {
entry35:
  %t61 = alloca i64
  %t64 = alloca i64
  %t75 = alloca i64
  store i64 %p0_0, ptr %t61
  %t62 = load i64, ptr %t61
  %t63 = icmp eq i64 %t62, 0
  br i1 %t63, label %match.body38, label %match.next39
match.body38:
  store i64 100, ptr %t64
  br label %ret36
match.next39:
  %t65 = load i64, ptr %t61
  %t66 = icmp eq i64 %t65, 1
  %t67 = load i64, ptr %t61
  %t68 = icmp eq i64 %t67, 2
  %t69 = or i1 %t66, %t68
  br i1 %t69, label %match.body40, label %match.next41
match.body40:
  store i64 200, ptr %t64
  br label %ret36
match.next41:
  %t70 = load i64, ptr %t61
  %t71 = icmp sle i64 3, %t70
  %t72 = load i64, ptr %t61
  %t73 = icmp slt i64 %t72, 10
  %t74 = and i1 %t71, %t73
  br i1 %t74, label %match.body42, label %match.next43
match.body42:
  store i64 300, ptr %t64
  br label %ret36
match.next43:
  %t76 = load i64, ptr %t61
  store i64 %t76, ptr %t75
  %t77 = load i64, ptr %t75
  store i64 %t77, ptr %t64
  br label %ret36
match.end37:
  %t78 = sub i64 0, 1
  store i64 %t78, ptr %t64
  br label %ret36
ret36:
  %t79 = load i64, ptr %t64
  ret i64 %t79
}
define i64 @"example.com/enums.pairSum"({i64, i64} %p0_0) uwtable {
entry44:
  %t80 = alloca {i64, i64}
  %t82 = alloca i64
  %t84 = alloca i64
  %t90 = alloca i64
  store {i64, i64} %p0_0, ptr %t80
  %t81 = getelementptr inbounds {i64, i64}, ptr %t80, i32 0, i32 0
  %t83 = getelementptr inbounds {i64, i64}, ptr %t80, i32 0, i32 1
  %t85 = load i64, ptr %t81
  store i64 %t85, ptr %t82
  %t86 = load i64, ptr %t83
  store i64 %t86, ptr %t84
  %t87 = load i64, ptr %t82
  %t88 = load i64, ptr %t84
  %t89 = add i64 %t87, %t88
  store i64 %t89, ptr %t90
  br label %ret45
match.end46:
  store i64 0, ptr %t90
  br label %ret45
ret45:
  %t91 = load i64, ptr %t90
  ret i64 %t91
}
declare i8 @zyxrt_str_eq(ptr, i64, ptr, i64)
define i8 @"example.com/enums.same"({ ptr, i64 } %p0_0, { ptr, i64 } %p1_0) uwtable {
entry47:
  %t92 = alloca { ptr, i64 }
  %t93 = alloca { ptr, i64 }
  %t102 = alloca i8
  store { ptr, i64 } %p0_0, ptr %t92
  store { ptr, i64 } %p1_0, ptr %t93
  %t94 = load { ptr, i64 }, ptr %t92
  %t95 = load { ptr, i64 }, ptr %t93
  %t96 = extractvalue { ptr, i64 } %t94, 0
  %t97 = extractvalue { ptr, i64 } %t94, 1
  %t98 = extractvalue { ptr, i64 } %t95, 0
  %t99 = extractvalue { ptr, i64 } %t95, 1
  %t100 = call i8 @zyxrt_str_eq(ptr %t96, i64 %t97, ptr %t98, i64 %t99)
  %t101 = icmp ne i8 %t100, 0
  store i1 %t101, ptr %t102
  br label %ret48
ret48:
  %t103 = load i8, ptr %t102
  ret i8 %t103
}
declare ptr @zyxrt_region_alloc_current(i64, i64)
define ptr @"example.com/enums.divide"(i64 %p0_0, i64 %p1_0) uwtable {
entry49:
  %t104 = alloca i64
  %t105 = alloca i64
  %t114 = alloca ptr
  store i64 %p0_0, ptr %t104
  store i64 %p1_0, ptr %t105
  %t106 = load i64, ptr %t105
  %t107 = icmp eq i64 %t106, 0
  br i1 %t107, label %if.then51, label %if.else52
if.then51:
  %t108 = call ptr @zyxrt_region_alloc_current(i64 24, i64 8)
  store i8 1, ptr %t108
  %t109 = getelementptr inbounds %s.1, ptr %t108, i32 0, i32 1
  %t110 = getelementptr inbounds {{ ptr, i64 }}, ptr %t109, i32 0, i32 0
  %t111 = getelementptr inbounds [4 x i8], ptr @.str.0, i64 0, i64 0
  %t112 = insertvalue { ptr, i64 } undef, ptr %t111, 0
  %t113 = insertvalue { ptr, i64 } %t112, i64 4, 1
  store { ptr, i64 } %t113, ptr %t110
  store ptr %t108, ptr %t114
  br label %ret50
if.else52:
  br label %if.end53
if.end53:
  %t115 = call ptr @zyxrt_region_alloc_current(i64 24, i64 8)
  store i8 0, ptr %t115
  %t116 = getelementptr inbounds %s.1, ptr %t115, i32 0, i32 1
  %t117 = getelementptr inbounds {i64}, ptr %t116, i32 0, i32 0
  %t118 = load i64, ptr %t104
  %t119 = load i64, ptr %t105
  %t120 = sdiv i64 %t118, %t119
  store i64 %t120, ptr %t117
  store ptr %t115, ptr %t114
  br label %ret50
ret50:
  %t121 = load ptr, ptr %t114
  ret ptr %t121
}
define i64 @"example.com/enums.report"(ptr %p0_0) uwtable {
entry54:
  %t122 = alloca ptr
  %t128 = alloca i64
  %t130 = alloca i64
  %t137 = alloca { ptr, i64 }
  store ptr %p0_0, ptr %t122
  %t123 = load ptr, ptr %t122
  %t124 = load i8, ptr %t123
  %t125 = icmp eq i8 %t124, 0
  %t126 = getelementptr inbounds %s.1, ptr %t123, i32 0, i32 1
  %t127 = getelementptr inbounds {i64}, ptr %t126, i32 0, i32 0
  br i1 %t125, label %match.body57, label %match.next58
match.body57:
  %t129 = load i64, ptr %t127
  store i64 %t129, ptr %t128
  %t131 = load i64, ptr %t128
  store i64 %t131, ptr %t130
  br label %ret55
match.next58:
  %t132 = load ptr, ptr %t122
  %t133 = load i8, ptr %t132
  %t134 = icmp eq i8 %t133, 1
  %t135 = getelementptr inbounds %s.1, ptr %t132, i32 0, i32 1
  %t136 = getelementptr inbounds {{ ptr, i64 }}, ptr %t135, i32 0, i32 0
  br i1 %t134, label %match.body59, label %match.next60
match.body59:
  %t138 = load { ptr, i64 }, ptr %t136
  store { ptr, i64 } %t138, ptr %t137
  store i64 0, ptr %t130
  br label %ret55
match.next60:
  br label %match.end56
match.end56:
  %t139 = sub i64 0, 1
  store i64 %t139, ptr %t130
  br label %ret55
ret55:
  %t140 = load i64, ptr %t130
  ret i64 %t140
}
define void @"example.com/enums.main"() uwtable {
entry61:
  %t144 = alloca ptr
  %t151 = alloca ptr
  %t155 = alloca ptr
  %t141 = call ptr @zyxrt_region_alloc_current(i64 24, i64 8)
  store i8 1, ptr %t141
  %t142 = getelementptr inbounds %s.0, ptr %t141, i32 0, i32 1
  %t143 = getelementptr inbounds {double}, ptr %t142, i32 0, i32 0
  store double 2.0, ptr %t143
  store ptr %t141, ptr %t144
  %t145 = load ptr, ptr %t144
  %t146 = call double @"example.com/enums.Shape.Area"(ptr %t145)
  call void @"std/fmt.PrintF64"(double %t146)
  call void @"std/fmt.PrintNl"()
  %t147 = call ptr @zyxrt_region_alloc_current(i64 24, i64 8)
  store i8 2, ptr %t147
  %t148 = getelementptr inbounds %s.0, ptr %t147, i32 0, i32 1
  %t149 = getelementptr inbounds {double, double}, ptr %t148, i32 0, i32 0
  store double 3.0, ptr %t149
  %t150 = getelementptr inbounds {double, double}, ptr %t148, i32 0, i32 1
  store double 5.0, ptr %t150
  store ptr %t147, ptr %t151
  %t152 = load ptr, ptr %t151
  %t153 = call double @"example.com/enums.Shape.Area"(ptr %t152)
  call void @"std/fmt.PrintF64"(double %t153)
  call void @"std/fmt.PrintNl"()
  %t154 = call ptr @zyxrt_region_alloc_current(i64 24, i64 8)
  store i8 0, ptr %t154
  store ptr %t154, ptr %t155
  %t156 = load ptr, ptr %t155
  %t157 = call double @"example.com/enums.Shape.Area"(ptr %t156)
  call void @"std/fmt.PrintF64"(double %t157)
  call void @"std/fmt.PrintNl"()
  %t158 = call ptr @zyxrt_region_alloc_current(i64 1, i64 1)
  store i8 1, ptr %t158
  %t159 = call i64 @"example.com/enums.Color.Code"(ptr %t158)
  call void @"std/fmt.PrintI64"(i64 %t159)
  call void @"std/fmt.PrintNl"()
  %t160 = call i64 @"example.com/enums.classify"(i64 0)
  call void @"std/fmt.PrintI64"(i64 %t160)
  call void @"std/fmt.PrintNl"()
  %t161 = call i64 @"example.com/enums.classify"(i64 2)
  call void @"std/fmt.PrintI64"(i64 %t161)
  call void @"std/fmt.PrintNl"()
  %t162 = call i64 @"example.com/enums.classify"(i64 7)
  call void @"std/fmt.PrintI64"(i64 %t162)
  call void @"std/fmt.PrintNl"()
  %t163 = call i64 @"example.com/enums.classify"(i64 42)
  call void @"std/fmt.PrintI64"(i64 %t163)
  call void @"std/fmt.PrintNl"()
  %t164 = insertvalue {i64, i64} undef, i64 3, 0
  %t165 = insertvalue {i64, i64} %t164, i64 4, 1
  %t166 = call i64 @"example.com/enums.pairSum"({i64, i64} %t165)
  call void @"std/fmt.PrintI64"(i64 %t166)
  call void @"std/fmt.PrintNl"()
  %t167 = getelementptr inbounds [3 x i8], ptr @.str.1, i64 0, i64 0
  %t168 = insertvalue { ptr, i64 } undef, ptr %t167, 0
  %t169 = insertvalue { ptr, i64 } %t168, i64 3, 1
  %t170 = getelementptr inbounds [3 x i8], ptr @.str.1, i64 0, i64 0
  %t171 = insertvalue { ptr, i64 } undef, ptr %t170, 0
  %t172 = insertvalue { ptr, i64 } %t171, i64 3, 1
  %t173 = call i8 @"example.com/enums.same"({ ptr, i64 } %t169, { ptr, i64 } %t172)
  call void @"std/fmt.PrintBool"(i8 %t173)
  %t174 = getelementptr inbounds [3 x i8], ptr @.str.1, i64 0, i64 0
  %t175 = insertvalue { ptr, i64 } undef, ptr %t174, 0
  %t176 = insertvalue { ptr, i64 } %t175, i64 3, 1
  %t177 = getelementptr inbounds [3 x i8], ptr @.str.2, i64 0, i64 0
  %t178 = insertvalue { ptr, i64 } undef, ptr %t177, 0
  %t179 = insertvalue { ptr, i64 } %t178, i64 3, 1
  %t180 = call i8 @"example.com/enums.same"({ ptr, i64 } %t176, { ptr, i64 } %t179)
  call void @"std/fmt.PrintBool"(i8 %t180)
  call void @"std/fmt.PrintNl"()
  %t181 = call ptr @"example.com/enums.divide"(i64 10, i64 2)
  %t182 = call i64 @"example.com/enums.report"(ptr %t181)
  call void @"std/fmt.PrintI64"(i64 %t182)
  call void @"std/fmt.PrintNl"()
  %t183 = call ptr @"example.com/enums.divide"(i64 10, i64 0)
  %t184 = call i64 @"example.com/enums.report"(ptr %t183)
  call void @"std/fmt.PrintI64"(i64 %t184)
  call void @"std/fmt.PrintNl"()
  br label %ret62
ret62:
  ret void
}
define internal void @zyx.globals() {
globals63:
  ret void
}
declare i32 @zyxrt_run_main(ptr, ptr)
define internal void @zyx.main.thunk(ptr %arg) {
  call void @zyx.globals()
  call void @"example.com/enums.main"()
  ret void
}
define i32 @main() {
  call i32 @zyxrt_run_main(ptr @zyx.main.thunk, ptr null)
  ret i32 0
}
