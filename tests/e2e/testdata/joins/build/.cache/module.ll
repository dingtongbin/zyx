; zyx
target triple = "x86_64-pc-windows-msvc"
%s.0 = type {i8, {{i64}, [24 x i8]}}
%s.1 = type {{ ptr, i64 }, { ptr, i64 }}
@.str.0 = private unnamed_addr constant [4 x i8] c"boom"
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
define i64 @"example.com/joins.work"(i64 %p0_0) uwtable {
entry17:
  %t19 = alloca i64
  %t20 = alloca i64
  %t23 = alloca i64
  %t24 = alloca i64
  %t25 = alloca i64
  %t34 = alloca i64
  store i64 %p0_0, ptr %t19
  store i64 0, ptr %t20
  %t21 = load i64, ptr %t19
  %t22 = sub i64 %t21, 0
  store i64 0, ptr %t23
  store i64 %t22, ptr %t24
  br label %forin.cond19
forin.cond19:
  %t26 = load i64, ptr %t23
  %t27 = load i64, ptr %t24
  %t28 = icmp slt i64 %t26, %t27
  br i1 %t28, label %forin.body20, label %forin.end22
forin.body20:
  %t29 = add i64 0, %t26
  store i64 %t29, ptr %t25
  %t30 = load i64, ptr %t20
  %t31 = load i64, ptr %t25
  %t32 = add i64 %t30, %t31
  store i64 %t32, ptr %t20
  br label %forin.step21
forin.step21:
  %t33 = add i64 %t26, 1
  store i64 %t33, ptr %t23
  br label %forin.cond19
forin.end22:
  %t35 = load i64, ptr %t20
  store i64 %t35, ptr %t34
  br label %ret18
ret18:
  %t36 = load i64, ptr %t34
  ret i64 %t36
}
declare void @zyxrt_panic(ptr, i64)
define i64 @"example.com/joins.boom"() uwtable {
entry23:
  %t42 = alloca i64
  %t37 = getelementptr inbounds [4 x i8], ptr @.str.0, i64 0, i64 0
  %t38 = insertvalue { ptr, i64 } undef, ptr %t37, 0
  %t39 = insertvalue { ptr, i64 } %t38, i64 4, 1
  %t40 = extractvalue { ptr, i64 } %t39, 0
  %t41 = extractvalue { ptr, i64 } %t39, 1
  call void @zyxrt_panic(ptr %t40, i64 %t41)
  unreachable
ret24:
  %t43 = load i64, ptr %t42
  ret i64 %t43
}
declare ptr @zyxrt_region_alloc_current(i64, i64)
declare void @zyxrt_coro_set_result(ptr)
define internal void @zyx.spawn.1(ptr %arg) {
thunk27:
  %t46 = getelementptr inbounds {i64}, ptr %arg, i32 0, i32 0
  %t47 = load i64, ptr %t46
  %t48 = call i64 @"example.com/joins.work"(i64 %t47)
  %t49 = inttoptr i64 %t48 to ptr
  call void @zyxrt_coro_set_result(ptr %t49)
  ret void
}
declare ptr @zyxrt_coro_spawn(ptr, ptr)
declare ptr @zyxrt_coro_join(ptr, ptr)
define internal void @zyx.spawn.2(ptr %arg) {
thunk37:
  %t78 = call i64 @"example.com/joins.boom"()
  %t79 = inttoptr i64 %t78 to ptr
  call void @zyxrt_coro_set_result(ptr %t79)
  ret void
}
define void @"example.com/joins.main"() uwtable {
entry25:
  %t51 = alloca ptr
  %t53 = alloca ptr
  %t68 = alloca i64
  %t75 = alloca ptr
  %t81 = alloca ptr
  %t83 = alloca ptr
  %t98 = alloca i64
  %t105 = alloca ptr
  %t44 = call ptr @zyxrt_region_alloc_current(i64 8, i64 8)
  %t45 = getelementptr inbounds {i64}, ptr %t44, i32 0, i32 0
  store i64 5, ptr %t45
  %t50 = call ptr @zyxrt_coro_spawn(ptr @zyx.spawn.1, ptr %t44)
  store ptr %t50, ptr %t51
  %t52 = load ptr, ptr %t51
  %t54 = call ptr @zyxrt_coro_join(ptr %t52, ptr %t53)
  %t55 = load ptr, ptr %t53
  %t56 = call ptr @zyxrt_region_alloc_current(i64 40, i64 8)
  %t57 = icmp ne ptr %t55, null
  %t58 = select i1 %t57, i8 1, i8 0
  store i8 %t58, ptr %t56
  br i1 %t57, label %join.err29, label %join.ok30
join.ok30:
  %t59 = ptrtoint ptr %t54 to i64
  %t60 = getelementptr inbounds %s.0, ptr %t56, i32 0, i32 1
  %t61 = getelementptr inbounds {i64}, ptr %t60, i32 0, i32 0
  store i64 %t59, ptr %t61
  br label %join.end31
join.err29:
  %t62 = getelementptr inbounds %s.0, ptr %t56, i32 0, i32 1
  %t63 = getelementptr inbounds {%s.1}, ptr %t62, i32 0, i32 0
  store ptr %t55, ptr %t63
  br label %join.end31
join.end31:
  %t64 = load i8, ptr %t56
  %t65 = icmp eq i8 %t64, 0
  %t66 = getelementptr inbounds %s.0, ptr %t56, i32 0, i32 1
  %t67 = getelementptr inbounds {i64}, ptr %t66, i32 0, i32 0
  br i1 %t65, label %match.body33, label %match.next34
match.body33:
  %t69 = load i64, ptr %t67
  store i64 %t69, ptr %t68
  %t70 = load i64, ptr %t68
  call void @"std/fmt.PrintI64"(i64 %t70)
  br label %match.end32
match.next34:
  %t71 = load i8, ptr %t56
  %t72 = icmp eq i8 %t71, 1
  %t73 = getelementptr inbounds %s.0, ptr %t56, i32 0, i32 1
  %t74 = getelementptr inbounds {%s.1}, ptr %t73, i32 0, i32 0
  br i1 %t72, label %match.body35, label %match.next36
match.body35:
  store ptr %t74, ptr %t75
  %t76 = sub i64 0, 1
  call void @"std/fmt.PrintI64"(i64 %t76)
  br label %match.end32
match.next36:
  br label %match.end32
match.end32:
  call void @"std/fmt.PrintNl"()
  %t77 = call ptr @zyxrt_region_alloc_current(i64 0, i64 1)
  %t80 = call ptr @zyxrt_coro_spawn(ptr @zyx.spawn.2, ptr %t77)
  store ptr %t80, ptr %t81
  %t82 = load ptr, ptr %t81
  %t84 = call ptr @zyxrt_coro_join(ptr %t82, ptr %t83)
  %t85 = load ptr, ptr %t83
  %t86 = call ptr @zyxrt_region_alloc_current(i64 40, i64 8)
  %t87 = icmp ne ptr %t85, null
  %t88 = select i1 %t87, i8 1, i8 0
  store i8 %t88, ptr %t86
  br i1 %t87, label %join.err39, label %join.ok40
join.ok40:
  %t89 = ptrtoint ptr %t84 to i64
  %t90 = getelementptr inbounds %s.0, ptr %t86, i32 0, i32 1
  %t91 = getelementptr inbounds {i64}, ptr %t90, i32 0, i32 0
  store i64 %t89, ptr %t91
  br label %join.end41
join.err39:
  %t92 = getelementptr inbounds %s.0, ptr %t86, i32 0, i32 1
  %t93 = getelementptr inbounds {%s.1}, ptr %t92, i32 0, i32 0
  store ptr %t85, ptr %t93
  br label %join.end41
join.end41:
  %t94 = load i8, ptr %t86
  %t95 = icmp eq i8 %t94, 0
  %t96 = getelementptr inbounds %s.0, ptr %t86, i32 0, i32 1
  %t97 = getelementptr inbounds {i64}, ptr %t96, i32 0, i32 0
  br i1 %t95, label %match.body43, label %match.next44
match.body43:
  %t99 = load i64, ptr %t97
  store i64 %t99, ptr %t98
  %t100 = load i64, ptr %t98
  call void @"std/fmt.PrintI64"(i64 %t100)
  br label %match.end42
match.next44:
  %t101 = load i8, ptr %t86
  %t102 = icmp eq i8 %t101, 1
  %t103 = getelementptr inbounds %s.0, ptr %t86, i32 0, i32 1
  %t104 = getelementptr inbounds {%s.1}, ptr %t103, i32 0, i32 0
  br i1 %t102, label %match.body45, label %match.next46
match.body45:
  store ptr %t104, ptr %t105
  %t106 = sub i64 0, 2
  call void @"std/fmt.PrintI64"(i64 %t106)
  br label %match.end42
match.next46:
  br label %match.end42
match.end42:
  call void @"std/fmt.PrintNl"()
  br label %ret26
ret26:
  ret void
}
define internal void @zyx.globals() {
globals47:
  ret void
}
declare i32 @zyxrt_run_main(ptr, ptr)
define internal void @zyx.main.thunk(ptr %arg) {
  call void @zyx.globals()
  call void @"example.com/joins.main"()
  ret void
}
define i32 @main() {
  call i32 @zyxrt_run_main(ptr @zyx.main.thunk, ptr null)
  ret i32 0
}
