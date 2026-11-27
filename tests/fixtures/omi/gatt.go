package omi

// GATT characteristics the simulator serves, lowercase as the app names them.
// Sources: omi/firmware/omi/src/lib/core/{transport.c,button.c,storage.c},
// src/haptic.c, Zephyr's BAS and DIS services as omi.conf configures them.
const (
	AudioData     = "19b10001-e8f2-537e-4f6c-d104768a1214" // read, notify
	AudioCodec    = "19b10002-e8f2-537e-4f6c-d104768a1214" // read
	Features      = "19b10021-e8f2-537e-4f6c-d104768a1214" // read
	TimeSyncWrite = "19b10031-e8f2-537e-4f6c-d104768a1214" // write: u32 LE epoch seconds
	TimeSyncRead  = "19b10032-e8f2-537e-4f6c-d104768a1214" // read: u32 LE epoch seconds
	Button        = "23ba7925-0000-1000-7450-346eac492e92" // read, notify
	StorageData   = "30295781-4301-eabd-2904-2849adfeae43" // write, notify (storage commands and data)
	StorageRead   = "30295782-4301-eabd-2904-2849adfeae43" // read, notify (storage status)
	Haptic        = "cab1ab96-2ea5-4f4d-bb56-874b72cfc984" // write: 1/2/3
	BatteryLevel  = "00002a19-0000-1000-8000-00805f9b34fb" // read, notify (BAS 0x180F)
	ModelNumber   = "00002a24-0000-1000-8000-00805f9b34fb" // DIS 0x180A
	SerialNumber  = "00002a25-0000-1000-8000-00805f9b34fb"
	FirmwareRev   = "00002a26-0000-1000-8000-00805f9b34fb"
	HardwareRev   = "00002a27-0000-1000-8000-00805f9b34fb"
	Manufacturer  = "00002a29-0000-1000-8000-00805f9b34fb"
)

// Numbers the firmware fixes.
const (
	AttDefaultMTU     = 23  // Bluetooth Core: ATT_MTU before an exchange
	DeviceMaxMTU      = 498 // omi.conf CONFIG_BT_L2CAP_TX_MTU
	MinimalPacketSize = 100 // config.h MINIMAL_PACKET_SIZE: no live audio below this MTU
	attHeader         = 3   // ATT opcode + handle (transport.c ATT_NOTIFICATION_HEADER_SIZE)
	fragmentHeader    = 3   // packet number LE16 + index (NET_BUFFER_HEADER_SIZE)

	recordAudio   = 440 // sd_card.h MAX_WRITE_SIZE: packed frames per record
	recordBytes   = 444 // RAW_AUDIO_PACKET_BYTES: BE32 timestamp + 440
	chunkRecords  = 36  // storage.c STORAGE_CHUNK_COUNT: records read per batch
	infoMinMTU    = 44  // storage.c STORAGE_INFO_MIN_ATT_MTU
	tsUptimeFlag  = 0x80000000
	tsMinUTC      = 1700000000 // custody.h OMI_TS_MIN_UTC
	custodyCaps   = 0x0F       // OMI_CAP_SUPPORTED_MASK
	capLive       = 0x02       // OMI_CAP_LIVE_PERSIST
	custodyVer    = 1
	rawMetaSect   = 64 // sd_card.c RAW_META_SECTORS
	rawBatchSect  = 32 // RAW_BATCH_SECTORS
	sectorBytes   = 512
	recordsPerBat = (rawBatchSect*sectorBytes - 32) / recordBytes // RAW_PACKETS_PER_BATCH
	maxStorage    = 0x1E000000                                    // sd_card.h MAX_STORAGE_BYTES
)

// Button values (button.c).
const (
	SingleTap = 1
	DoubleTap = 2
	LongTap   = 3
	Press     = 4
	Release   = 5
)

// features is the u32 the features characteristic reads for omi.conf's
// enabled set: button, battery, haptic, offline storage, and the always-on
// LED dimming, mic gain, device name, capture mute and power control.
const features = 1<<2 | 1<<3 | 1<<5 | 1<<6 | 1<<7 | 1<<8 | 1<<9 | 1<<10 | 1<<11

var notifiable = map[string]bool{AudioData: true, Button: true, StorageData: true, StorageRead: true, BatteryLevel: true}

// ATT errors the simulator answers with.
const (
	ErrReadNotPermitted  = "read not permitted"
	ErrWriteNotPermitted = "write not permitted"
	ErrNotFound          = "attribute not found"
	ErrInvalidLength     = "invalid attribute value length"
	ErrNotNotifiable     = "characteristic does not notify"
	ErrNotConnected      = "not connected"
)
