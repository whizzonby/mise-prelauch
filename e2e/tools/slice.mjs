// Dev helper: slices a tall screenshot into viewable pieces. node tools/slice.mjs <png> <prefix> [sliceHeight] [outWidth]
import { createRequire } from "node:module";
import { readdirSync } from "node:fs";
const dir = "E:/MISE PRELAUNCH/mise/node_modules/.pnpm";
const sharpDir = readdirSync(dir).find((d) => d.startsWith("sharp@"));
const sharp = createRequire(`${dir}/${sharpDir}/node_modules/sharp/`)("sharp");
const [file, prefix, H = "1800", W = "1100"] = process.argv.slice(2);
const m = await sharp(file).metadata();
let n = 0;
for (let i = 0; i * +H < m.height; i++, n++) {
  await sharp(file).extract({ left: 0, top: i * +H, width: m.width, height: Math.min(+H, m.height - i * +H) }).resize({ width: Math.min(+W, m.width) }).jpeg({ quality: 78 }).toFile(`${prefix}-${i}.jpg`);
}
console.log(m.width, m.height, n, "slices");
