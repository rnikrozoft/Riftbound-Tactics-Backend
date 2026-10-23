# คำแปลจาก Excel

ไฟล์หลักที่ admin แก้คือ `data/localization.xlsx` ใน repository backend

1. เปิด sheet `Translations` แล้วแก้เฉพาะ `en` และ `th` (คอลัมน์สีเหลือง)
2. คง `key`, `type`, `source_en`, `source_th` ไว้ ชื่อคอลัมน์และชื่อ sheet ใช้ในการนำเข้า
3. เก็บตัวแปร `{0}`, `{1}` ให้ครบ สามารถสลับตำแหน่งในประโยคได้ คงช่องว่างหัว/ท้ายของข้อความ `fragments`
4. บันทึกเป็น `.xlsx` ห้ามใส่สูตรใน sheet นี้
5. ตรวจด้วย `go run ./cmd/localizationcheck data/localization.xlsx`
6. build และ deploy backend ตามขั้นตอนปกติ (local: `docker compose up -d --build nakama`)

Docker build ตรวจไฟล์ก่อนสร้าง plugin การนำเข้า database เกิดเมื่อ backend เริ่มทำงานหลัง deploy เพราะช่วง build ยังไม่เชื่อม database

backend เก็บคำแปลใน Nakama Storage collection `riftbound_config`, key `localization` ให้สิทธิ์อ่าน/เขียนตรงเฉพาะระบบ พร้อมเวอร์ชัน SHA-256 จากเนื้อหาคำแปล ไม่เปลี่ยนเวอร์ชันเพราะเปลี่ยนรูปแบบ Excel หรือเรียงแถวใหม่

RPC `localization_bootstrap` ต้องมี session ผู้เล่น ส่ง `cached_version` ถ้าเวอร์ชันตรงกันจะได้เฉพาะ version ถ้าต่างกันจะได้ document ใหม่ด้วย การนำเข้าซ้ำไฟล์เดิมไม่เขียน database ใหม่

client โหลด cache ตอนเปิดเกมและตรวจเวอร์ชันเมื่อโหลดข้อมูลผู้เล่น เก็บ cache แยกตาม server พร้อม checksum เขียนแบบไฟล์ชั่วคราวแล้วเปลี่ยนชื่อ ถ้าเน็ตขาด backend เก่าไม่รองรับ หรือ cache เสีย จะใช้ cache ที่ถูกต้องหรือคำแปลในตัวเกมแทน การเขียน cache ไม่สำเร็จไม่ทำให้ login ล้มเหลว

ไฟล์ JSON ใน client เป็น fallback สำหรับการเปิดเกมครั้งแรกแบบ offline ไม่ใช่ไฟล์ที่ admin ต้องแก้ เมื่อเปลี่ยนถ้อยคำที่มีอยู่แล้วใน Excel ไม่ต้อง build client ใหม่ แต่การเพิ่มข้อความ UI หรือภาษาใหม่ยังต้องปรับ client
