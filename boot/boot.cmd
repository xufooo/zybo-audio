# ZYBO Rev B audio player boot script (run by distro_bootcmd)
echo "== ZYBO audio: loading FPGA bitstream =="
fatload mmc 0:1 0x100000 system.bit
fpga loadb 0 0x100000 ${filesize}

echo "== ZYBO audio: loading kernel =="
fatload mmc 0:1 0x3000000 uImage
fatload mmc 0:1 0x2A00000 zybo-audio.dtb

setenv bootargs console=ttyPS0,115200 root=/dev/mmcblk0p2 rootwait rw
bootm 0x3000000 - 0x2A00000
