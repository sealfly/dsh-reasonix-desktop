// 分析全屏截图中 #0153e5（Reasonix logo 蓝）像素簇：输出每个蓝色区域的位置和尺寸
// 用法：node analyze-blue.js <png路径>
const fs = require('fs');
const zlib = require('zlib');

// 极简 PNG 解码（只支持 8-bit RGBA/RGB 非隔行）
function decodePNG(buf) {
  // 找 IHDR
  let off = 8;
  let width = 0, height = 0, bitDepth = 0, colorType = 0;
  while (off < buf.length) {
    const len = buf.readUInt32BE(off);
    const type = buf.toString('ascii', off + 4, off + 8);
    if (type === 'IHDR') {
      width = buf.readUInt32BE(off + 8);
      height = buf.readUInt32BE(off + 12);
      bitDepth = buf[off + 16];
      colorType = buf[off + 17];
    }
    if (type === 'IDAT') break;
    off += 12 + len;
  }
  if (!width || !height) throw new Error('no IHDR');
  // 收集 IDAT
  const chunks = [];
  off = 8;
  while (off < buf.length) {
    const len = buf.readUInt32BE(off);
    const type = buf.toString('ascii', off + 4, off + 8);
    if (type === 'IDAT') chunks.push(buf.slice(off + 8, off + 8 + len));
    if (type === 'IEND') break;
    off += 12 + len;
  }
  const raw = zlib.inflateSync(Buffer.concat(chunks));
  const bpp = colorType === 6 ? 4 : colorType === 2 ? 3 : 1;
  const stride = width * bpp;
  const px = Buffer.alloc(width * height * 4);
  let pos = 0;
  let prev = Buffer.alloc(stride);
  for (let y = 0; y < height; y++) {
    const filter = raw[pos++];
    const line = raw.slice(pos, pos + stride); pos += stride;
    const recon = Buffer.alloc(stride);
    for (let x = 0; x < stride; x++) {
      const a = x >= bpp ? recon[x - bpp] : 0;
      const b = prev[x];
      const c = x >= bpp ? prev[x - bpp] : 0;
      let v = line[x];
      if (filter === 1) v = (v + a) & 0xff;
      else if (filter === 2) v = (v + b) & 0xff;
      else if (filter === 3) v = (v + ((a + b) >> 1)) & 0xff;
      else if (filter === 4) {
        const p = a + b - c;
        const pa = Math.abs(p - a), pb = Math.abs(p - b), pc = Math.abs(p - c);
        const pr = (pa <= pb && pa <= pc) ? a : (pb <= pc ? b : c);
        v = (v + pr) & 0xff;
      }
      recon[x] = v;
    }
    for (let x = 0; x < width; x++) {
      const si = x * bpp, di = (y * width + x) * 4;
      if (colorType === 6) { px[di] = recon[si]; px[di+1] = recon[si+1]; px[di+2] = recon[si+2]; px[di+3] = recon[si+3]; }
      else if (colorType === 2) { px[di] = recon[si]; px[di+1] = recon[si+1]; px[di+2] = recon[si+2]; px[di+3] = 255; }
      else { px[di] = recon[si]; px[di+1] = recon[si]; px[di+2] = recon[si]; px[di+3] = 255; }
    }
    prev = recon;
  }
  return { width, height, px };
}

const file = process.argv[2];
if (!file || !fs.existsSync(file)) { console.log('用法: node analyze-blue.js <png>'); process.exit(1); }
const img = decodePNG(fs.readFileSync(file));
const { width, height, px } = img;
console.log(`尺寸: ${width}x${height}`);

// 蓝色像素（#0153e5 ≈ RGB(1,83,229)），容差匹配
const isBlue = (r, g, b) => b > 150 && b > r + 80 && b > g + 60 && r < 100 && g < 150;
// 聚类：每 8px 采样 + 区域合并
const marks = new Uint8Array(width * height);
let count = 0, minX = width, maxX = 0, minY = height, maxY = 0;
for (let y = 0; y < height; y += 2) {
  for (let x = 0; x < width; x += 2) {
    const i = (y * width + x) * 4;
    if (isBlue(px[i], px[i+1], px[i+2])) {
      marks[y * width + x] = 1; count++;
      if (x < minX) minX = x; if (x > maxX) maxX = x;
      if (y < minY) minY = y; if (y > maxY) maxY = y;
    }
  }
}
console.log(`蓝色像素采样点: ${count}`);
if (count === 0) { console.log('未找到蓝色像素'); process.exit(0); }

// 按行分布（找离散区域）
const rowCounts = new Array(height).fill(0);
for (let y = 0; y < height; y++) for (let x = 0; x < width; x++) if (marks[y * width + x]) rowCounts[y]++;
// 找连续行带
const bands = []; let inBand = false, bandStart = 0;
for (let y = 0; y < height; y++) {
  if (rowCounts[y] > 0 && !inBand) { inBand = true; bandStart = y; }
  if (rowCounts[y] === 0 && inBand) { inBand = false; bands.push([bandStart, y - 1]); }
}
if (inBand) bands.push([bandStart, height - 1]);
console.log(`蓝色区域（连续行带）数量: ${bands.length}`);
bands.forEach((b, i) => {
  let bMinX = width, bMaxX = 0, bCount = 0;
  for (let y = b[0]; y <= b[1]; y++) for (let x = 0; x < width; x++) {
    if (marks[y * width + x]) { bCount++; if (x < bMinX) bMinX = x; if (x > bMaxX) bMaxX = x; }
  }
  console.log(`  区域${i+1}: y=${b[0]}..${b[1]} (高${b[1]-b[0]+1}px) x=${bMinX}..${bMaxX} (宽${bMaxX-bMinX+1}px) 采样${bCount}`);
});
