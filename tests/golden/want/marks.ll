; zyx
target triple = "x86_64-pc-windows-msvc"
@.str.0 = private unnamed_addr constant [6 x i8] c"first\00"
@.str.1 = private unnamed_addr constant [7 x i8] c"second\00"
@.str.2 = private unnamed_addr constant [5 x i8] c"body\00"
@.str.3 = private unnamed_addr constant [13 x i8] c"fell through\00"
@.str.4 = private unnamed_addr constant [8 x i8] c"unwound\00"
@.str.5 = private unnamed_addr constant [5 x i8] c"bang\00"
@.str.6 = private unnamed_addr constant [9 x i8] c"survived\00"
@.str.7 = private unnamed_addr constant [30 x i8] c"example.com/golden-marks.boom\00"
@.str.8 = private unnamed_addr constant [19 x i8] c"main.zyx:24:1-27:2\00"
@.str.9 = private unnamed_addr constant [30 x i8] c"example.com/golden-marks.main\00"
@.str.10 = private unnamed_addr constant [19 x i8] c"main.zyx:29:1-36:2\00"
@.str.11 = private unnamed_addr constant [30 x i8] c"example.com/golden-marks.note\00"
@.str.12 = private unnamed_addr constant [19 x i8] c"main.zyx:10:1-12:2\00"
@.str.13 = private unnamed_addr constant [30 x i8] c"example.com/golden-marks.work\00"
@.str.14 = private unnamed_addr constant [19 x i8] c"main.zyx:14:1-22:2\00"
declare void @"std/fmt.Print"({ ptr, i64 })
declare void @"std/fmt.Println"({ ptr, i64 })
declare void @"std/fmt.PrintI64"(i64)
declare void @"std/fmt.PrintU64"(i64)
declare void @"std/fmt.PrintF64"(double)
declare void @"std/fmt.PrintBool"(i8)
declare void @"std/fmt.PrintChar"(i32)
declare void @"std/fmt.PrintNl"()
declare void @"std/fmt.Fprint"(ptr, { ptr, i64 })
declare void @"std/fmt.Fprintln"(ptr, { ptr, i64 })
declare ptr @zyxrt_region_current()
declare void @zyxrt_stack_grow(ptr)
define void @"example.com/golden-marks.note"({ ptr, i64 } %p0_0) uwtable {
entry1:
  %t1 = alloca { ptr, i64 }
  %t3 = alloca ptr
  %t5 = alloca i8
  %t6 = ptrtoint ptr %t5 to i64
  %t7 = sub i64 %t6, 536
  %t8 = inttoptr i64 %t7 to ptr
  %t9 = call i64 asm sideeffect "movq %gs:16, $0", "=r,~{dirflag},~{fpsr},~{flags}"()
  %t10 = icmp ult i64 %t7, %t9
  br i1 %t10, label %stack3, label %body4
stack3:
  call void @zyxrt_stack_grow(ptr %t8)
  br label %body4
body4:
  store { ptr, i64 } %p0_0, ptr %t1
  %t2 = call ptr @zyxrt_region_current()
  store ptr %t2, ptr %t3
  %t4 = load { ptr, i64 }, ptr %t1
  call void @"std/fmt.Println"({ ptr, i64 } %t4)
  br label %ret2
ret2:
  ret void
}
declare ptr @zyxrt_defer_mark()
declare ptr @zyxrt_region_alloc_current(i64, i64)
define internal void @zyx.defer.1(ptr %arg) uwtable {
defer7:
  %t21 = getelementptr inbounds {{ ptr, i64 }}, ptr %arg, i32 0, i32 0
  %t22 = load { ptr, i64 }, ptr %t21
  call void @"example.com/golden-marks.note"({ ptr, i64 } %t22)
  ret void
}
declare void @zyxrt_defer_push(ptr, ptr)
define internal void @zyx.defer.2(ptr %arg) uwtable {
defer9:
  %t28 = getelementptr inbounds {{ ptr, i64 }}, ptr %arg, i32 0, i32 0
  %t29 = load { ptr, i64 }, ptr %t28
  call void @"example.com/golden-marks.note"({ ptr, i64 } %t29)
  ret void
}
declare void @zyxrt_defer_run(ptr)
define void @"example.com/golden-marks.work"(i8 %p0_0) uwtable {
entry5:
  %t11 = alloca i8
  %t13 = alloca ptr
  %t15 = alloca ptr
  %t39 = alloca i8
  %t40 = ptrtoint ptr %t39 to i64
  %t41 = sub i64 %t40, 529
  %t42 = inttoptr i64 %t41 to ptr
  %t43 = call i64 asm sideeffect "movq %gs:16, $0", "=r,~{dirflag},~{fpsr},~{flags}"()
  %t44 = icmp ult i64 %t41, %t43
  br i1 %t44, label %stack14, label %body15
stack14:
  call void @zyxrt_stack_grow(ptr %t42)
  br label %body15
body15:
  store i8 %p0_0, ptr %t11
  %t12 = call ptr @zyxrt_region_current()
  store ptr %t12, ptr %t13
  %t14 = call ptr @zyxrt_defer_mark()
  store ptr %t14, ptr %t15
  %t16 = call ptr @zyxrt_region_alloc_current(i64 16, i64 8)
  %t17 = getelementptr inbounds [5 x i8], ptr @.str.0, i64 0, i64 0
  %t18 = insertvalue { ptr, i64 } undef, ptr %t17, 0
  %t19 = insertvalue { ptr, i64 } %t18, i64 5, 1
  %t20 = getelementptr inbounds {{ ptr, i64 }}, ptr %t16, i32 0, i32 0
  store { ptr, i64 } %t19, ptr %t20
  call void @zyxrt_defer_push(ptr @zyx.defer.1, ptr %t16)
  %t23 = call ptr @zyxrt_region_alloc_current(i64 16, i64 8)
  %t24 = getelementptr inbounds [6 x i8], ptr @.str.1, i64 0, i64 0
  %t25 = insertvalue { ptr, i64 } undef, ptr %t24, 0
  %t26 = insertvalue { ptr, i64 } %t25, i64 6, 1
  %t27 = getelementptr inbounds {{ ptr, i64 }}, ptr %t23, i32 0, i32 0
  store { ptr, i64 } %t26, ptr %t27
  call void @zyxrt_defer_push(ptr @zyx.defer.2, ptr %t23)
  %t30 = load i8, ptr %t11
  %t31 = icmp ne i8 %t30, 0
  br i1 %t31, label %if.then11, label %if.else12
if.then11:
  %t32 = getelementptr inbounds [4 x i8], ptr @.str.2, i64 0, i64 0
  %t33 = insertvalue { ptr, i64 } undef, ptr %t32, 0
  %t34 = insertvalue { ptr, i64 } %t33, i64 4, 1
  call void @"std/fmt.Println"({ ptr, i64 } %t34)
  br label %ret6
if.else12:
  br label %if.end13
if.end13:
  %t35 = getelementptr inbounds [12 x i8], ptr @.str.3, i64 0, i64 0
  %t36 = insertvalue { ptr, i64 } undef, ptr %t35, 0
  %t37 = insertvalue { ptr, i64 } %t36, i64 12, 1
  call void @"std/fmt.Println"({ ptr, i64 } %t37)
  br label %ret6
ret6:
  %t38 = load ptr, ptr %t15
  call void @zyxrt_defer_run(ptr %t38)
  ret void
}
define internal void @zyx.defer.3(ptr %arg) uwtable {
defer18:
  %t54 = getelementptr inbounds {{ ptr, i64 }}, ptr %arg, i32 0, i32 0
  %t55 = load { ptr, i64 }, ptr %t54
  call void @"example.com/golden-marks.note"({ ptr, i64 } %t55)
  ret void
}
declare void @zyxrt_panic(ptr, i64)
define void @"example.com/golden-marks.boom"() uwtable {
entry16:
  %t46 = alloca ptr
  %t48 = alloca ptr
  %t62 = alloca i8
  %t63 = ptrtoint ptr %t62 to i64
  %t64 = sub i64 %t63, 528
  %t65 = inttoptr i64 %t64 to ptr
  %t66 = call i64 asm sideeffect "movq %gs:16, $0", "=r,~{dirflag},~{fpsr},~{flags}"()
  %t67 = icmp ult i64 %t64, %t66
  br i1 %t67, label %stack20, label %body21
stack20:
  call void @zyxrt_stack_grow(ptr %t65)
  br label %body21
body21:
  %t45 = call ptr @zyxrt_region_current()
  store ptr %t45, ptr %t46
  %t47 = call ptr @zyxrt_defer_mark()
  store ptr %t47, ptr %t48
  %t49 = call ptr @zyxrt_region_alloc_current(i64 16, i64 8)
  %t50 = getelementptr inbounds [7 x i8], ptr @.str.4, i64 0, i64 0
  %t51 = insertvalue { ptr, i64 } undef, ptr %t50, 0
  %t52 = insertvalue { ptr, i64 } %t51, i64 7, 1
  %t53 = getelementptr inbounds {{ ptr, i64 }}, ptr %t49, i32 0, i32 0
  store { ptr, i64 } %t52, ptr %t53
  call void @zyxrt_defer_push(ptr @zyx.defer.3, ptr %t49)
  %t56 = getelementptr inbounds [4 x i8], ptr @.str.5, i64 0, i64 0
  %t57 = insertvalue { ptr, i64 } undef, ptr %t56, 0
  %t58 = insertvalue { ptr, i64 } %t57, i64 4, 1
  %t59 = extractvalue { ptr, i64 } %t58, 0
  %t60 = extractvalue { ptr, i64 } %t58, 1
  call void @zyxrt_panic(ptr %t59, i64 %t60)
  unreachable
ret17:
  %t61 = load ptr, ptr %t48
  call void @zyxrt_defer_run(ptr %t61)
  ret void
}
declare ptr @zyxrt_region_new()
declare void @zyxrt_region_enter(ptr)
define internal void @zyx.spawn.4(ptr %arg) {
thunk24:
  call void @"example.com/golden-marks.boom"()
  ret void
}
declare ptr @zyxrt_coro_spawn(ptr, ptr, i32)
declare void @zyxrt_region_wait(ptr)
declare void @zyxrt_region_leave()
declare void @zyxrt_region_free(ptr)
define void @"example.com/golden-marks.main"() uwtable {
entry22:
  %t69 = alloca ptr
  %t71 = alloca ptr
  %t77 = alloca i8
  %t78 = ptrtoint ptr %t77 to i64
  %t79 = sub i64 %t78, 528
  %t80 = inttoptr i64 %t79 to ptr
  %t81 = call i64 asm sideeffect "movq %gs:16, $0", "=r,~{dirflag},~{fpsr},~{flags}"()
  %t82 = icmp ult i64 %t79, %t81
  br i1 %t82, label %stack26, label %body27
stack26:
  call void @zyxrt_stack_grow(ptr %t80)
  br label %body27
body27:
  %t68 = call ptr @zyxrt_region_current()
  store ptr %t68, ptr %t69
  call void @"example.com/golden-marks.work"(i8 1)
  call void @"example.com/golden-marks.work"(i8 0)
  %t70 = call ptr @zyxrt_region_new()
  store ptr %t70, ptr %t71
  call void @zyxrt_region_enter(ptr %t70)
  %t72 = call ptr @zyxrt_region_alloc_current(i64 0, i64 1)
  %t73 = call ptr @zyxrt_coro_spawn(ptr @zyx.spawn.4, ptr %t72, i32 0)
  call void @zyxrt_region_wait(ptr %t70)
  call void @zyxrt_region_leave()
  call void @zyxrt_region_free(ptr %t70)
  %t74 = getelementptr inbounds [8 x i8], ptr @.str.6, i64 0, i64 0
  %t75 = insertvalue { ptr, i64 } undef, ptr %t74, 0
  %t76 = insertvalue { ptr, i64 } %t75, i64 8, 1
  call void @"std/fmt.Println"({ ptr, i64 } %t76)
  br label %ret23
ret23:
  ret void
}
define void @"zyx.init.example.com/golden-marks"() {
init28:
  ret void
}
declare void @"zyx.init.std/fmt"()
define internal void @zyx.globals() {
  call void @"zyx.init.std/fmt"()
  call void @"zyx.init.example.com/golden-marks"()
  ret void
}
declare i32 @zyxrt_run_main(ptr, ptr)
define internal void @zyx.main.thunk(ptr %arg) {
  call void @zyx.globals()
  call void @"example.com/golden-marks.main"()
  ret void
}
define i32 @main() {
  %zyx.runtime = call i32 @zyxrt_run_main(ptr @zyx.main.thunk, ptr null)
  ret i32 %zyx.runtime
}
declare void @zyxrt_frames_register(ptr, i64)
@zyx.frames = internal constant [4 x { ptr, ptr, ptr }] [
  { ptr, ptr, ptr } { ptr @"example.com/golden-marks.boom", ptr @.str.7, ptr @.str.8 },
  { ptr, ptr, ptr } { ptr @"example.com/golden-marks.main", ptr @.str.9, ptr @.str.10 },
  { ptr, ptr, ptr } { ptr @"example.com/golden-marks.note", ptr @.str.11, ptr @.str.12 },
  { ptr, ptr, ptr } { ptr @"example.com/golden-marks.work", ptr @.str.13, ptr @.str.14 }
]
define internal void @zyx.frames.init() {
  call void @zyxrt_frames_register(ptr @zyx.frames, i64 4)
  ret void
}
@llvm.global_ctors = appending global [1 x { i32, ptr, ptr }] [{ i32, ptr, ptr } { i32 65535, ptr @zyx.frames.init, ptr null }]
