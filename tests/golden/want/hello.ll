; zyx
target triple = "x86_64-pc-windows-msvc"
@.str.0 = private unnamed_addr constant [13 x i8] c"hello, world\00"
@.str.1 = private unnamed_addr constant [30 x i8] c"example.com/golden-hello.main\00"
@.str.2 = private unnamed_addr constant [18 x i8] c"main.zyx:9:1-11:2\00"
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
define internal void @"example.com/golden-hello.main"() uwtable inlinehint {
entry1:
  %t2 = alloca ptr
  %t6 = alloca i8
  %t7 = ptrtoint ptr %t6 to i64
  %t8 = sub i64 %t7, 520
  %t9 = inttoptr i64 %t8 to ptr
  %t10 = call i64 asm sideeffect "movq %gs:16, $0", "=r,~{dirflag},~{fpsr},~{flags}"()
  %t11 = icmp ult i64 %t8, %t10
  br i1 %t11, label %stack3, label %body4
stack3:
  call void @zyxrt_stack_grow(ptr %t9)
  br label %body4
body4:
  %t1 = call ptr @zyxrt_region_current()
  store ptr %t1, ptr %t2
  %t3 = getelementptr inbounds [12 x i8], ptr @.str.0, i64 0, i64 0
  %t4 = insertvalue { ptr, i64 } undef, ptr %t3, 0
  %t5 = insertvalue { ptr, i64 } %t4, i64 12, 1
  call void @"std/fmt.Println"({ ptr, i64 } %t5)
  br label %ret2
ret2:
  ret void
}
define void @"zyx.init.example.com/golden-hello"() {
init5:
  ret void
}
declare void @"zyx.init.std/fmt"()
define internal void @zyx.globals() {
  call void @"zyx.init.std/fmt"()
  call void @"zyx.init.example.com/golden-hello"()
  ret void
}
declare i32 @zyxrt_run_main(ptr, ptr)
define internal void @zyx.main.thunk(ptr %arg) {
  call void @zyx.globals()
  call void @"example.com/golden-hello.main"()
  ret void
}
define i32 @main() {
  %zyx.runtime = call i32 @zyxrt_run_main(ptr @zyx.main.thunk, ptr null)
  ret i32 %zyx.runtime
}
declare void @zyxrt_frames_register(ptr, i64)
@zyx.frames = internal constant [1 x { ptr, ptr, ptr }] [
  { ptr, ptr, ptr } { ptr @"example.com/golden-hello.main", ptr @.str.1, ptr @.str.2 }
]
define internal void @zyx.frames.init() {
  call void @zyxrt_frames_register(ptr @zyx.frames, i64 1)
  ret void
}
@llvm.global_ctors = appending global [1 x { i32, ptr, ptr }] [{ i32, ptr, ptr } { i32 65535, ptr @zyx.frames.init, ptr null }]
