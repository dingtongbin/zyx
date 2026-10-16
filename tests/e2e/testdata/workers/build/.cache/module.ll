; zyx
target triple = "x86_64-pc-windows-msvc"
@.str.0 = private unnamed_addr constant [5 x i8] c"start"
@.str.1 = private unnamed_addr constant [4 x i8] c" one"
@.str.2 = private unnamed_addr constant [4 x i8] c" two"
@.str.3 = private unnamed_addr constant [3 x i8] c"end"
declare ptr @zyxrt_region_current()
declare void @zyxrt_print_str(ptr, i64)
define void @"std/fmt.Print"({ ptr, i64 } %p0_0) uwtable {
entry1:
  %t1 = alloca { ptr, i64 }
  %t3 = alloca ptr
  store { ptr, i64 } %p0_0, ptr %t1
  %t2 = call ptr @zyxrt_region_current()
  store ptr %t2, ptr %t3
  %t4 = load { ptr, i64 }, ptr %t1
  %t5 = extractvalue { ptr, i64 } %t4, 0
  %t6 = extractvalue { ptr, i64 } %t4, 1
  call void @zyxrt_print_str(ptr %t5, i64 %t6)
  br label %ret2
ret2:
  ret void
}
declare void @zyxrt_print_nl()
define void @"std/fmt.Println"({ ptr, i64 } %p0_0) uwtable {
entry3:
  %t7 = alloca { ptr, i64 }
  %t9 = alloca ptr
  store { ptr, i64 } %p0_0, ptr %t7
  %t8 = call ptr @zyxrt_region_current()
  store ptr %t8, ptr %t9
  %t10 = load { ptr, i64 }, ptr %t7
  %t11 = extractvalue { ptr, i64 } %t10, 0
  %t12 = extractvalue { ptr, i64 } %t10, 1
  call void @zyxrt_print_str(ptr %t11, i64 %t12)
  call void @zyxrt_print_nl()
  br label %ret4
ret4:
  ret void
}
declare void @zyxrt_print_i64(i64)
define void @"std/fmt.PrintI64"(i64 %p0_0) uwtable {
entry5:
  %t13 = alloca i64
  %t15 = alloca ptr
  store i64 %p0_0, ptr %t13
  %t14 = call ptr @zyxrt_region_current()
  store ptr %t14, ptr %t15
  %t16 = load i64, ptr %t13
  call void @zyxrt_print_i64(i64 %t16)
  br label %ret6
ret6:
  ret void
}
declare void @zyxrt_print_u64(i64)
define void @"std/fmt.PrintU64"(i64 %p0_0) uwtable {
entry7:
  %t17 = alloca i64
  %t19 = alloca ptr
  store i64 %p0_0, ptr %t17
  %t18 = call ptr @zyxrt_region_current()
  store ptr %t18, ptr %t19
  %t20 = load i64, ptr %t17
  call void @zyxrt_print_u64(i64 %t20)
  br label %ret8
ret8:
  ret void
}
declare void @zyxrt_print_f64(double)
define void @"std/fmt.PrintF64"(double %p0_0) uwtable {
entry9:
  %t21 = alloca double
  %t23 = alloca ptr
  store double %p0_0, ptr %t21
  %t22 = call ptr @zyxrt_region_current()
  store ptr %t22, ptr %t23
  %t24 = load double, ptr %t21
  call void @zyxrt_print_f64(double %t24)
  br label %ret10
ret10:
  ret void
}
declare void @zyxrt_print_bool(i8)
define void @"std/fmt.PrintBool"(i8 %p0_0) uwtable {
entry11:
  %t25 = alloca i8
  %t27 = alloca ptr
  store i8 %p0_0, ptr %t25
  %t26 = call ptr @zyxrt_region_current()
  store ptr %t26, ptr %t27
  %t28 = load i8, ptr %t25
  call void @zyxrt_print_bool(i8 %t28)
  br label %ret12
ret12:
  ret void
}
declare void @zyxrt_print_char(i32)
define void @"std/fmt.PrintChar"(i32 %p0_0) uwtable {
entry13:
  %t29 = alloca i32
  %t31 = alloca ptr
  store i32 %p0_0, ptr %t29
  %t30 = call ptr @zyxrt_region_current()
  store ptr %t30, ptr %t31
  %t32 = load i32, ptr %t29
  call void @zyxrt_print_char(i32 %t32)
  br label %ret14
ret14:
  ret void
}
define void @"std/fmt.PrintNl"() uwtable {
entry15:
  %t34 = alloca ptr
  %t33 = call ptr @zyxrt_region_current()
  store ptr %t33, ptr %t34
  call void @zyxrt_print_nl()
  br label %ret16
ret16:
  ret void
}
define void @"example.com/workers.report"(i64 %p0_0, { ptr, i64 } %p1_0) uwtable {
entry17:
  %t35 = alloca i64
  %t36 = alloca { ptr, i64 }
  %t38 = alloca ptr
  store i64 %p0_0, ptr %t35
  store { ptr, i64 } %p1_0, ptr %t36
  %t37 = call ptr @zyxrt_region_current()
  store ptr %t37, ptr %t38
  %t39 = load i64, ptr %t35
  call void @"std/fmt.PrintI64"(i64 %t39)
  %t40 = load { ptr, i64 }, ptr %t36
  call void @"std/fmt.Print"({ ptr, i64 } %t40)
  call void @"std/fmt.PrintNl"()
  br label %ret18
ret18:
  ret void
}
declare ptr @zyxrt_region_new()
declare void @zyxrt_region_enter(ptr)
declare ptr @zyxrt_region_alloc_current(i64, i64)
define internal void @zyx.spawn.1(ptr %arg) {
thunk21:
  %t54 = getelementptr inbounds {i64, { ptr, i64 }}, ptr %arg, i32 0, i32 0
  %t55 = load i64, ptr %t54
  %t56 = getelementptr inbounds {i64, { ptr, i64 }}, ptr %arg, i32 0, i32 1
  %t57 = load { ptr, i64 }, ptr %t56
  call void @"example.com/workers.report"(i64 %t55, { ptr, i64 } %t57)
  ret void
}
declare ptr @zyxrt_coro_spawn(ptr, ptr)
define internal void @zyx.spawn.2(ptr %arg) {
thunk23:
  %t65 = getelementptr inbounds {i64, { ptr, i64 }}, ptr %arg, i32 0, i32 0
  %t66 = load i64, ptr %t65
  %t67 = getelementptr inbounds {i64, { ptr, i64 }}, ptr %arg, i32 0, i32 1
  %t68 = load { ptr, i64 }, ptr %t67
  call void @"example.com/workers.report"(i64 %t66, { ptr, i64 } %t68)
  ret void
}
declare void @zyxrt_region_wait(ptr)
declare void @zyxrt_region_leave()
declare void @zyxrt_region_free(ptr)
define void @"example.com/workers.main"() uwtable {
entry19:
  %t42 = alloca ptr
  %t47 = alloca ptr
  %t41 = call ptr @zyxrt_region_current()
  store ptr %t41, ptr %t42
  %t43 = getelementptr inbounds [5 x i8], ptr @.str.0, i64 0, i64 0
  %t44 = insertvalue { ptr, i64 } undef, ptr %t43, 0
  %t45 = insertvalue { ptr, i64 } %t44, i64 5, 1
  call void @"std/fmt.Println"({ ptr, i64 } %t45)
  %t46 = call ptr @zyxrt_region_new()
  store ptr %t46, ptr %t47
  call void @zyxrt_region_enter(ptr %t46)
  %t48 = call ptr @zyxrt_region_alloc_current(i64 24, i64 8)
  %t49 = getelementptr inbounds {i64, { ptr, i64 }}, ptr %t48, i32 0, i32 0
  store i64 1, ptr %t49
  %t50 = getelementptr inbounds [4 x i8], ptr @.str.1, i64 0, i64 0
  %t51 = insertvalue { ptr, i64 } undef, ptr %t50, 0
  %t52 = insertvalue { ptr, i64 } %t51, i64 4, 1
  %t53 = getelementptr inbounds {i64, { ptr, i64 }}, ptr %t48, i32 0, i32 1
  store { ptr, i64 } %t52, ptr %t53
  %t58 = call ptr @zyxrt_coro_spawn(ptr @zyx.spawn.1, ptr %t48)
  %t59 = call ptr @zyxrt_region_alloc_current(i64 24, i64 8)
  %t60 = getelementptr inbounds {i64, { ptr, i64 }}, ptr %t59, i32 0, i32 0
  store i64 2, ptr %t60
  %t61 = getelementptr inbounds [4 x i8], ptr @.str.2, i64 0, i64 0
  %t62 = insertvalue { ptr, i64 } undef, ptr %t61, 0
  %t63 = insertvalue { ptr, i64 } %t62, i64 4, 1
  %t64 = getelementptr inbounds {i64, { ptr, i64 }}, ptr %t59, i32 0, i32 1
  store { ptr, i64 } %t63, ptr %t64
  %t69 = call ptr @zyxrt_coro_spawn(ptr @zyx.spawn.2, ptr %t59)
  call void @zyxrt_region_wait(ptr %t46)
  call void @zyxrt_region_leave()
  call void @zyxrt_region_free(ptr %t46)
  %t70 = getelementptr inbounds [3 x i8], ptr @.str.3, i64 0, i64 0
  %t71 = insertvalue { ptr, i64 } undef, ptr %t70, 0
  %t72 = insertvalue { ptr, i64 } %t71, i64 3, 1
  call void @"std/fmt.Println"({ ptr, i64 } %t72)
  br label %ret20
ret20:
  ret void
}
define internal void @zyx.globals() {
globals25:
  ret void
}
declare i32 @zyxrt_run_main(ptr, ptr)
define internal void @zyx.main.thunk(ptr %arg) {
  call void @zyx.globals()
  call void @"example.com/workers.main"()
  ret void
}
define i32 @main() {
  call i32 @zyxrt_run_main(ptr @zyx.main.thunk, ptr null)
  ret i32 0
}
