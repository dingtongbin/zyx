/* probe_caught raises the two exceptions a library raises and answers itself -
   the code a C++ library gives its own exception (0xE06D7363, what MSVC uses) and
   the thread-naming exception a debugger's convention uses (0x406D1388) - and
   answers how many of them its handler took. */

int probe_caught(void);
