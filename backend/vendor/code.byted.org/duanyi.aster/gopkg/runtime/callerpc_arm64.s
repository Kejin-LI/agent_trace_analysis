
#include "go_asm.h"
#include "funcdata.h"
#include "textflag.h"

TEXT ·physicalCallerPC(SB), NOSPLIT | NOFRAME, $0-16
    NO_LOCAL_POINTERS

    MOVD 8(RSP), R1 
    CMP  ZR    , R1
    BGT  slow_path
    MOVD R30   , R4
    SUB  $1, R4, R4
    MOVD R4    , 16(RSP)
    RET 

slow_path:
    MOVD R29   , R4
    MOVD (g)   , R2
    MOVD 8(g)  , R3
    JMP  ret

loop:
    CMP  ZR    , R4
    BEQ  overflow
    CMP  R2    , R4
    BLE  overflow
    CMP  R3    , R4
    BGE  overflow
    MOVD (R4)  , R4
    SUB  $1, R1, R1

ret:
    CMP  $1    , R1
    BGT  loop
    MOVD 8(R4) , R4
    SUB  $1, R4, R4
    MOVD R4    , 16(RSP)
    RET

overflow:
    MOVD ZR    , 16(RSP)
    RET


TEXT ·logicCallerPC(SB), NOSPLIT, $56-16
    NO_LOCAL_POINTERS

    MOVD 88(RSP), R1 
    CMP  ZR    , R1
    BGT  slow_path 
    MOVD R30, R4
    SUB  $1, R4, R4
    MOVD R4    , 96(RSP)
    RET

slow_path:
    // SUB  $512, RSP, R5
    // MOVD 16(g)  , R6
    // CMP  R6, R5
    // BLS  stack_grow
    MOVD R29   , R4 
    MOVD (R4)  , R4
    MOVD (g)   , R2
    MOVD 8(g)  , R3
    JMP  check

loop:
    CMP  ZR    , R4    
    BEQ  overflow
    CMP  R2    , R4
    BLE  overflow
    CMP  R3    , R4
    BGE  overflow  

skip:
    MOVD (R4)  , R4
    SUB  $1, R1, R1

check:
    MOVD R4    , 40(RSP)
    MOVD R2    , 48(RSP)
    MOVD R3    , 56(RSP)
    MOVD 8(R4) , R5
    SUB  $1, R5, R5
    MOVD R5    , 8(RSP)
    SUB  $1, R1, R1
    MOVD R1    , 16(RSP)
    CALL ·countInlined(SB)
    MOVD 32(RSP), R4
    CMP  ZR    , R4 
    BNE  ret2
    MOVD 24(RSP), R1
    MOVD 40(RSP), R4
    MOVD 48(RSP), R2
    MOVD 56(RSP), R3
    ADD  $1, R1, R1
    CMP  $1    , R1 
    BGT  loop

ret:
    MOVD 8(R4) , R4
ret2:
    SUB  $1, R4, R4
    MOVD R4    , 96(RSP)
    RET  

overflow:
    MOVD $0    , 96(RSP)
    RET

// stack_grow:
//     CALL runtime·morestack_noctxt<>(SB)
//     MOVD 88(RSP), R1 
//     JMP  slow_path
