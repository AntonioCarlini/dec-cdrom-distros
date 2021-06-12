$!� - Do not alter this line - it stops IBM PCs executing LOGIN.COM !
$!
$ v = F$VERIFY(0)
$ SET NOON
$ SET CONTROL=(T,Y)
$!
$! This DCL command procedure saves some data from an OpenVMS CONDIST CDROM.
$! Specifically it saves the CD_CONTENTS.DAT (which lists all the products) and any SPDs.
$!
$! It starts by identifying the CDROM itself and uses that to build an identifying name.
$! For an OpenVMS VAX CONDIST this will be of the form VAXVMS_CONDIST_yyyy_mm_nn where
$!   yyyy is the disc four digit year
$!     mm is the two digit month number (01 .. 12)
$!     nn is the two digit disc number (01 .. e.g. 12)
$!
$! The CDROM's [CDROM]CD_CONTENTS.DAT will then be copied to VAXVMS_CONDIST_yyyy_mm_nn_CD_CONTENTS.DAT in the target directory.
$! A subdirectory [.VAXVMS_CONDIST_yyyy_mm_nn] will be created in the target directory.
$! All text and PostScript SPDs on the CDROM will be copied into this subdirectory.
$!
$! Note that on the early CONDISTS (at least until NOV 1989) the CD_CONTENTS.DAT on each CONDIST only listed products on that specific CDROM.
$! Later CONDISTS changed to list all the products and also listed all the CDROM volume names.
$! This command procedure only works with the latter format.
$!
$! Further note that early CONDISTs kept TXT and PS docs in [product.LINE_DOCS] and [product.POST_DOCS] subdirectories. This later
$! switched to keeping all the documents together in a [product.DOCUMENTATION] subdirectory. This command procedure copies from all
$! three possible locations and just ignores errors from whichever set does not exist.
$!
$! Inputs:
$!
$! P1 - CDROM device name, including the final colon
$! P2 - Where the output files and subdirectory should be placed
$!
$!
$ say := WRITE SYS$OUTPUT
$ dbg := "!"                  ! replace this with "''say'" to turn on debug
$!
$ dbg "P1=''P1'"
$ dbg "P2=''P2'"
$!
$ CDROM_DEVICE = P1
$ CDROM_CONTENTS_FILENAME = "''CDROM_DEVICE'[CDROM]CD_CONTENTS.DAT"
$ DESTINATION_DIRECTORY = P2
$!
$! Parse CD_CONTENTS.DAT on teh CDROM to work out which CDROM it is and write the final name ot 'OUTPUT_NAME'
$ CALL BUILD_CDROM_NAME 'CDROM_DEVICE' 'CDROM_CONTENTS_FILENAME' OUTPUT_NAME
$!
$ CONTENTS_FILENAME = DESTINATION_DIRECTORY + OUTPUT_NAME + "_CD_CONTENTS.DAT"
$ CALL COPY_CD_CONTENTS 'CDROM_CONTENTS_FILENAME' 'CONTENTS_FILENAME'
$!
$! Create an output subdirectory and copy SPDs to it
$ ROOT = F$ELEMENT(0, "]", DESTINATION_DIRECTORY)
$ ROOT_FOR_CDROM = "''ROOT.''OUTPUT_NAME']"
$ CREATE/LOG/DIRECTORY 'ROOT_FOR_CDROM'
$ COPY/LOG 'CDROM_DEVICE'[*.DOCUMENTATION]*_SPD.*, -
           'CDROM_DEVICE'[*.LINE_DOCS]*_SPD.TXT,   -
           'CDROM_DEVICE'[*.POST_DOCS]_SPD.PS -
                                               'ROOT_FOR_CDROM'
$ PURGE/LOG 'ROOT_FOR_CDROM'
$Done:
$ EXIT 1 + F$VERIFY(v)


$!+
$! Finds the CD_CONTENTS.DAT on the specified CDROM device and copies it to the
$! specified directory, using a fixed format destination filename.
$! If the destination file already exists, a suitable message is issued.
$!
$! P1 - filename of CDROM contents file
$! P2 - filename of destination for contents file
$!-
$COPY_CD_CONTENTS: SUBROUTINE
$ TARGET = F$SEARCH(P2)
$ IF TARGET .EQS. ""
$ THEN
$     COPY/LOG 'P1' 'P2'
$ ELSE
$     ! DIFFERENCES returns the same $STATUS for a identical files and different files.
$     ! CHECKSUM returns the same checksum for different files (e.g. if one has an extra blank line at the start)
$     ! PIPE the result of DIFF into SEARCH and hunt for the message that says there are no differences.
$     ! The only wrinkle is that now SEARCH returns a $STATUS of %X10000001 for "no diffs" instead of %X00000001    
$     PIPE DIFFERENCES 'P1' 'P2' -
         | SEARCH/OUTPUT=NLA0: SYS$INPUT "Number of difference records found: 0"
$     RESULT = $STATUS .AND. %X0FFFFFFF
$     dbg "res ''result'"
$     IF RESULT .EQ. %X00000001
$     THEN
$         say "Not copied: Identical file to ''P1' already present: ''P2'"
$     ELSE
$         say "Files differ: ''P1' and ''P2'"
$     ENDIF
$ ENDIF
$ RETURN
$ENDSUBROUTINE

$!+
$! Builds a name that represents the CDROM specified by the device named in P1.
$!
$! The destination filename will be of the form:
$!    VAXVMS_CONDIST_yyyy_mm_vv_CD_CONTENTS
$! where
$!    yyyy: is the four digit year (found from CD_CONTENTS.DAT)
$!    mm:   is the two digit month (found from CD_CONTENTS.DAT)
$!    vv:   is the two digit disc number in the set (found by comparing the CDROM device volume label with the list in CD_CONTENTS.DAT)
$!
$! We look for the following lines in CD_CONTENTS.DAT:
$! This line means this is a CONDIST CD:
$!   %TYPE CONDIST
$!
$! This line lists the labels of the discs in the set. We can use this to determine part of the name of our output file:
$!   %LABEL CDBIN04MAR21,CDBIN04MAR22,CDBIN04MAR23,CDBIN04MAR24,CDBIN04MAR25,CDBIN04MAR26
$!
$! This line gives us the CDROM date and is used to form part of the name of the output file:
$!   %RELEASE_DATE March 1992
$!
$! This line indicates the end of the data prelude in CD_CONTENTS.DAT. We don't need anything beyond this line:
$!   %END_OF_DATA
$!
$! P1: device holding the CDROM
$! P2: filename on the CDROM of the CD_CONTENTS.DAT file
$! P3: symbol that will hold the resulting CDROM name
$!-
$BUILD_CDROM_NAME: SUBROUTINE
$ dbg "in sub COPY_CD_CONTENTS: p1=[''p1'] p2=[''p2']"
$ CONDIST = 0
$ VOLUME_NAMES = ""
$ RELEASE_DATE = ""
$ IF F$LOGICAL("CONTENTS") .NES. "" THEN CLOSE CONTENTS   ! Close this file in case it has been let open by a previous run
$ CD_CONTENTS_FILE = P2
$ OPEN/READ/ERROR=NO_CONTENTS CONTENTS 'CD_CONTENTS_FILE'
$READ_LINE:
$ READ/END=EOF CONTENTS LINE
    $ IF LINE .EQS. "%END_OF_DATA" THEN GOTO DATA_GATHERED
$ IF LINE .EQS. "%TYPE CONDIST" THEN CONDIST = 1
$ FIRST_PART = F$ELEMENT(0, " ", LINE)
$ IF FIRST_PART .EQS. "%LABEL" THEN  VOLUME_NAMES = F$ELEMENT(1, " ", LINE)
$ IF FIRST_PART .EQS. "%RELEASE_DATE"
$ THEN
$    RELEASE_MONTH = F$ELEMENT(1, " ", LINE)
$    RELEASE_YEAR = F$ELEMENT(2, " ", LINE)
$ ENDIF
$ GOTO READ_LINE
$ NO_CONTENTS:
$ CLOSE CONTENTS
$ say "Unable to open CONDIST CD_CONTENTS at ''CD_CONTENTS_FILE'."
$ EXIT %X2
$DATA_GATHERED:
$ CLOSE CONTENTS
$! At this point we have found all the data we are going to get.
$ dbg "CONDIST = ", CONDIST
$ dbg "Volumes = [", VOLUME_NAMES, "]"
$ dbg "Rel date = [", RELEASE_YEAR, "/", RELEASE_MONTH, "]"
$!
$! Find the volume in the set by matching the device VOLNAM (volume name) against the list of volume names in the set
$!
$ ACTUAL_VOLUME_NAME = F$GETDVI(P1, "VOLNAM")
$ VOLUME_NUMBER = 1
$VOLUME_LOOP:
$ VOLNAM = F$ELEMENT(VOLUME_NUMBER - 1, ",", VOLUME_NAMES)
$ IF VOLNAM .EQS. "," THEN GOTO NO_SUCH_VOLUME
$ IF VOLNAM .EQS. ACTUAL_VOLUME_NAME THEN GOTO VOLUME_FOUND
$ VOLUME_NUMBER = VOLUME_NUMBER + 1
$ GOTO VOLUME_LOOP
$NO_SUCH_VOLUME:
$ say "Device ''P1' has volume label [''VOLNAM''] which does not appear in the list of CDROM labels: [''VOLUME_NAMES']"
$ EXIT %4
$VOLUME_FOUND:
$!
$! Turn a date like "March 1989" into "1989-03"
$ MONTH_TEXT = F$EDIT(RELEASE_MONTH, "UPCASE,TRIM")
$ YEAR = F$EDIT(RELEASE_YEAR, "UPCASE,TRIM")
$ MONTHS_OF_THE_YEAR = "JANUARY,FEBRUARY,MARCH,APRIL,MAY,JUNE,JULY,AUGUST,SEPTEMBER,OCTOBER,NOVEMBER,DECEMBER"
$ MONTH = 1
$ dbg "Looking for [''MONTH_TEXT']"
$MONTH_LOOP:
$ CANDIDATE_MONTH = F$ELEMENT(MONTH - 1, ",", MONTHS_OF_THE_YEAR)
$ IF CANDIDATE_MONTH .EQS. "," THEN GOTO NO_SUCH_MONTH
$ IF CANDIDATE_MONTH .EQS. MONTH_TEXT THEN GOTO MONTH_FOUND
$ dbg "month = ''MONTH'  candidate=[''candidate_moth']"
$ MONTH = MONTH + 1
$ GOTO MONTH_LOOP
$NO_SUCH_MONTH:
$ say "Month [''MONTH_TEXT'] is not a known month of the year: [''MONTHS_OF_THE_YEAR']"
$ EXIT %6
$MONTH_FOUND:
$!
$ dbg "CONDIST = ", CONDIST
$ dbg "YEAR = ", YEAR
$ dbg "MONTH = ", MONTH
$ dbg "VOLNUM = ", VOLUME_NUMBER
$ CDROM_NAME = "VAXVMS_CONDIST_"
$ CDROM_NAME = CDROM_NAME + YEAR + "_"
$ IF MONTH .LE. 9 THEN CDROM_NAME = CDROM_NAME + "0"
$ CDROM_NAME = CDROM_NAME + "''MONTH'" + "_"
$ IF VOLUME_NUMBER .LE. 9 THEN CDROM_NAME = CDROM_NAME + "0"
$ CDROM_NAME = CDROM_NAME + "''VOLUME_NUMBER'"
$ dbg "output file: [''CDROM_NAME']"
$!
$ 'P3' == CDROM_NAME
$ EXIT
$ENDSUBROUTINE
$!
