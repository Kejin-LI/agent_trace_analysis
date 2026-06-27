
#include "go_asm.h"
#include "funcdata.h"
#include "textflag.h"

TEXT ·MoreStack(SB), NOSPLIT, $0 - 8
    NO_LOCAL_POINTERS
_entry:
    MOVQ (TLS), R14
    MOVQ size+0(FP), R12
    NOTQ R12
    LEAQ (SP)(R12*1), R12
    CMPQ R12, 16(R14)
    JBE  _stack_grow
    RET
_stack_grow:
    CALL runtime·morestack_noctxt<>(SB)
    JMP  _entry


TEXT ·physicalCallerPC(SB), NOSPLIT | NOFRAME, $0-16
    NO_LOCAL_POINTERS

    MOVQ 8(SP) , BX
    CMPQ BX    , $0
    JA   slow_path 
    MOVQ (SP)  , AX
    DECQ AX
    MOVQ AX    , 16(SP)
    RET

slow_path:
    MOVQ BP    , AX 
    MOVQ (TLS) , R14
    MOVQ (R14) , CX
    MOVQ 8(R14), DX
    JMP  ret

loop:
    CMPQ AX    , $0
    JEQ  overflow
    CMPQ AX    , CX
    JLE  overflow
    CMPQ AX    , DX
    JAE  overflow  
    MOVQ (AX)  , AX
    DECQ BX

ret:
    CMPQ BX    , $1
    JA   loop
    MOVQ 8(AX) , AX
    DECQ AX
    MOVQ AX    , 16(SP)
    RET  

overflow:
    MOVQ $0   , 16(SP)
    RET


TEXT ·logicCallerPC(SB), NOSPLIT, $56-16
    NO_LOCAL_POINTERS

    MOVQ 72(SP), BX 
    CMPQ BX    , $0
    JA   slow_path 
    MOVQ 64(SP), AX
    DECQ AX
    MOVQ AX    , 80(SP)
    RET

slow_path:
    MOVQ (TLS) , R14
    LEAQ -512(SP), R12
    CMPQ R12, 16(R14)
    JBE  stack_grow
    MOVQ (R14) , CX
    MOVQ 8(R14), DX
    MOVQ BP    , AX 
    MOVQ (AX)  , AX
    JMP  check

loop:
    CMPQ AX    , $0
    JEQ  overflow
    CMPQ AX    , CX
    JLE  overflow
    CMPQ AX    , DX
    JAE  overflow  

skip:
    MOVQ (AX)  , AX
    DECQ BX

check:
    MOVQ AX    , 32(SP)
    MOVQ CX    , 40(SP)
    MOVQ DX    , 48(SP)
    MOVQ 8(AX) , DI
    DECQ DI
    MOVQ DI    , (SP)
    DECQ BX
    MOVQ BX    , 8(SP)
    CALL ·countInlined(SB)
    MOVQ 24(SP), AX
    CMPQ AX    , $0
    JNE  ret2
    MOVQ 16(SP), BX
    MOVQ 32(SP), AX
    MOVQ 40(SP), CX
    MOVQ 48(SP), DX
    INCQ BX
    CMPQ BX    , $1
    JA   loop

ret:
    MOVQ 8(AX) , AX
ret2:
    DECQ AX
    MOVQ AX    , 80(SP)
    RET  

overflow:
    MOVQ $0    , 80(SP)
    RET

stack_grow:
    CALL runtime·morestack_noctxt<>(SB)
    MOVQ 72(SP), BX
    JMP  slow_path

