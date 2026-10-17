// Official 2026 FRC AprilTags (tag36h11, IDs 1-32). Codes were read from FIRST's "2026 AprilTag Images and User Guide"
// (run/refs/first-2026) and match the AprilTag library's tag36h11 table. Bits use the AprilTag 3 layout order below;
// a set bit is a white cell.
export const TAG36H11 = {
  1: 0xdda664ca7,
  2: 0xdc4a1c821,
  3: 0xe17b470e9,
  4: 0xef91d01b1,
  5: 0xf429cdd73,
  6: 0x05da29225,
  7: 0x1106cba43,
  8: 0x223bed79d,
  9: 0x21f51213c,
  10: 0x33eb19ca6,
  11: 0x3f76eb0f8,
  12: 0x469a97414,
  13: 0x45dcfe0b0,
  14: 0x4a6465f72,
  15: 0x51801db96,
  16: 0x5eb946b4e,
  17: 0x68a7cc2ec,
  18: 0x6f0ba2652,
  19: 0x78765559d,
  20: 0x87b83d129,
  21: 0x86cc4a5c5,
  22: 0x8b64df90f,
  23: 0x9c577b611,
  24: 0xa3810f2f5,
  25: 0xaf4d75b83,
  26: 0xb59a03fef,
  27: 0xbb1096f85,
  28: 0xd1b92fc76,
  29: 0xd0dd509d2,
  30: 0xe2cfda160,
  31: 0x2ff497c63,
  32: 0x47240671b,
};

// Cell (x, y) of each code bit, inside the 1-cell black border (AprilTag 3 tag36h11 bit_x / bit_y).
const BIT_X = [1, 2, 3, 4, 5, 2, 3, 4, 3, 6, 6, 6, 6, 6, 5, 5, 5, 4, 6, 5, 4, 3, 2, 5, 4, 3, 4, 1, 1, 1, 1, 1, 2, 2, 2, 3];
const BIT_Y = [1, 1, 1, 1, 1, 2, 2, 2, 3, 1, 2, 3, 4, 5, 2, 3, 4, 3, 6, 6, 6, 6, 6, 5, 5, 5, 4, 6, 5, 4, 3, 2, 5, 4, 3, 4];

/** The tag as an 8x8 grid (black border included), row-major from the top-left; true = black. */
export function tagGrid(id) {
  const code = TAG36H11[id];
  const grid = Array.from({ length: 8 }, (_, y) => Array.from({ length: 8 }, (_, x) => x === 0 || y === 0 || x === 7 || y === 7));
  if (code === undefined) return grid;
  for (let i = 0; i < 36; i++) {
    const white = (BigInt(code) >> BigInt(35 - i)) & 1n;
    grid[BIT_Y[i]][BIT_X[i]] = white === 0n;
  }
  return grid;
}
