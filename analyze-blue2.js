const fs = require('fs');
const zlib = require('zlib');
function decodePNG(buf) {
  let off = 8, width = 0, height = 0, colorType = 0;
  const chunks = [];
  while (off < buf.length) {
    const len = buf.readUInt32BE(off);
    const type = buf.toString('ascii', off + 4, off + 8);
    if (type === 'IHDR') { width = buf.readUInt32BE(off + 8); height = buf.readUInt32BE(off + 12); colorType = buf[off + 17]; }
    if (type === 'IDAT') chunks.push(buf.slice(off + 8, off + 8 + len));
    if (type === 'IEND') break;
    off += 12 + len;
  }
  const raw = zlib.inflateSync(Buffer.concat(chunks));
  const bpp = colorType === 6 ? 4 : colorType === 2 ? 3 : 1;
  const stride = width * bpp;
  const px = Buffer.alloc(width * height * 4);
  let pos = 0, prev = Buffer.alloc(stride);
  for (let y = 0; y < height; y++) {
    const filter = raw[pos++];
    const line = raw.slice(pos, pos + stride); pos += stride;
    const recon = Buffer.alloc(stride);
    for (let x = 0; x < stride; x++) {
      const a = x >= bpp ? recon[x - bpp] : 0, b = prev[x], c = x >= bpp ? prev[x - bpp] : 0;
      let v = line[x];
      if (filter === 1) v = (v + a) & 0xff; else if (filter === 2) v = (v + b) & 0xff;
      else if (filter === 3) v = (v + ((a + b) >> 1)) & 0xff;
      else if (filter === 4) { const p = a + b - c, pa = Math.abs(p-a), pb = Math.abs(p-b), pc = Math.abs(p-c); v = (v + ((pa<=pb&&pa<=pc)?a:(pb<=pc?b:c))) & 0xff; }
      recon[x] = v;
    }
    for (let x = 0; x < width; x++) { const si = x*bpp, di = (y*width+x)*4; if (colorType===6){px[di]=recon[si];px[di+1]=recon[si+1];px[di+2]=recon[si+2];px[di+3]=recon[si+3];} else if (colorType===2){px[di]=recon[si];px[di+1]=recon[si+1];px[di+2]=recon[si+2];px[di+3]=255;} }
    prev = recon;
  }
  return { width, height, px };
}
const img = decodePNG(fs.readFileSync('screen-full.png'));
const { width, height, px } = img;
// 精确 logo 蓝：接近 RGB(1,83,229)
const isBlue = (r,g,b) => Math.abs(r-1)<50 && Math.abs(g-83)<60 && Math.abs(b-229)<60;
// 逐行统计蓝色像素
let total = 0; const lineBlue = new Array(height).fill(0);
for (let y = 0; y < height; y++) { for (let x = 0; x < width; x++) { const i=(y*width+x)*4; if (isBlue(px[i],px[i+1],px[i+2])) { lineBlue[y]++; total++; } } }
console.log('精确蓝色像素总数:', total);
// 找 y 带（连续行有蓝）
const bands = []; let inB=false, st=0;
for (let y=0;y<height;y++){ if(lineBlue[y]>0&&!inB){inB=true;st=y;} if(lineBlue[y]===0&&inB){inB=false;bands.push([st,y-1]);} }
if(inB)bands.push([st,height-1]);
console.log('蓝色行带数量:', bands.length);
bands.forEach(b=>{ let minX=width,maxX=0,c=0; for(let y=b[0];y<=b[1];y++){ if(lineBlue[y]===0)continue; for(let x=0;x<width;x++){const i=(y*width+x)*4; if(isBlue(px[i],px[i+1],px[i+2])){c++; if(x<minX)minX=x; if(x>maxX)maxX=x;}}} console.log(`  带 y=${b[0]}..${b[1]} x=${minX}..${maxX} 像素${c}`); });
