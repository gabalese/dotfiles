#define SPOOKI_TIME (iTime - iTimeFocus)
const float SPOOKI_VIBRANCY = 1.00;
const float SPOOKI_DIM = 1.0;

// added by SpookiUI: the treat animation only ever plays in the focused window.
// `iFocus` is 1.0 while focused, 0.0 while unfocused. When this window is
// unfocused we hide the effect and either:
//   - dim + desaturate the terminal (SPOOKI_DIM baked in), so the inactive
//     window is set apart from the focused one; or
//   - render the plain, untouched terminal when dimming is off.
vec4 spooki_out(vec3 res, vec4 term) {
    if (iFocus < 0.5) {
        if (SPOOKI_DIM > 0.5) {
            float g = dot(term.rgb, vec3(0.2126, 0.7152, 0.0722));  // luminance
            vec3 inactive = mix(term.rgb, vec3(g), 0.55) * 0.55;    // desaturate, dim
            return vec4(inactive, term.a);
        }
        return term;                                               // plain, no effect
    }
    vec3 fx = term.rgb + (res - term.rgb) * SPOOKI_VIBRANCY;    // scale the effect
    return vec4(min(fx, vec3(1.0)), term.a);
}

// SpookiUI treat: Jumper.
// A little red-capped, mustachioed hero hops along the ground beneath a row of
// scrolling question-blocks — styled after the 8-bit NES plumber. The sprite is
// snapped to a coarse pixel grid with hard edges for a chunky pixel-art look.
// Original SpookiUI shader: hand-built shapes, no game artwork. Drawn over dark
// background pixels only.

// Hard-edged solid primitives (coverage 0 or 1); coords are snapped to a coarse
// sprite-pixel grid before evaluation so the hero reads as chunky NES pixels.
float jb_box(vec2 p, vec2 b) {
    vec2 d = abs(p) - b;
    return step(max(d.x, d.y), 0.0);
}
float jb_disc(vec2 p, float r) {
    return step(dot(p, p), r * r);
}
void jb_lay(float m, vec3 c, inout vec3 col, inout float a) {
    m = clamp(m, 0.0, 1.0);
    col = mix(col, c, m);
    a = max(a, m);
}

// Paint the NES-plumber hero into (col,a). q is aspect-corrected and local to
// the hero: q.y points up, with the feet resting at q.y == 0.
void mario_draw(vec2 q, out vec3 col, out float a) {
    const vec3 RED   = vec3(0.86, 0.12, 0.10);   // cap + shirt
    const vec3 SKIN  = vec3(1.00, 0.74, 0.52);   // face + nose
    const vec3 BROWN = vec3(0.40, 0.22, 0.08);   // hair, mustache, boots
    const vec3 OVER  = vec3(0.18, 0.30, 0.90);   // blue overalls
    const vec3 BTN   = vec3(0.95, 0.85, 0.20);   // overall buttons
    const float PIX  = 0.0052;                   // sprite-pixel grid
    q = floor(q / PIX) * PIX + 0.5 * PIX;        // snap to the pixel grid
    col = vec3(0.0); a = 0.0;

    // Boots (front foot stepped forward).
    jb_lay(jb_box(q - vec2(-0.010, 0.006), vec2(0.011, 0.006)), BROWN, col, a);
    jb_lay(jb_box(q - vec2( 0.014, 0.006), vec2(0.011, 0.006)), BROWN, col, a);
    // Blue overalls: legs + torso.
    jb_lay(jb_box(q - vec2(0.002, 0.028), vec2(0.020, 0.017)), OVER, col, a);
    // Red shirt: shoulders + arms, a touch wider than the overalls.
    jb_lay(jb_box(q - vec2(0.002, 0.052), vec2(0.024, 0.009)), RED, col, a);
    // Overalls bib over the chest, with two buttons.
    jb_lay(jb_box(q - vec2(0.002, 0.050), vec2(0.012, 0.011)), OVER, col, a);
    jb_lay(jb_disc(q - vec2(-0.007, 0.048), 0.0034), BTN, col, a);
    jb_lay(jb_disc(q - vec2( 0.011, 0.048), 0.0034), BTN, col, a);
    // Face.
    jb_lay(jb_box(q - vec2(0.004, 0.070), vec2(0.018, 0.013)), SKIN, col, a);
    // Hair/sideburn at the back; mustache and nose at the front.
    jb_lay(jb_box(q - vec2(-0.016, 0.068), vec2(0.006, 0.011)), BROWN, col, a);
    jb_lay(jb_box(q - vec2( 0.010, 0.062), vec2(0.012, 0.004)), BROWN, col, a);
    jb_lay(jb_box(q - vec2( 0.020, 0.066), vec2(0.005, 0.005)), SKIN, col, a);
    // Eye.
    jb_lay(jb_box(q - vec2(0.008, 0.073), vec2(0.003, 0.005)), vec3(0.05, 0.05, 0.08), col, a);
    // Red cap: crown over the head + a brim jutting out the front.
    jb_lay(jb_box(q - vec2(0.000, 0.086), vec2(0.019, 0.008)), RED, col, a);
    jb_lay(jb_box(q - vec2(0.014, 0.080), vec2(0.016, 0.004)), RED, col, a);
}

// Small hash + a filled-disc primitive for the scenery.
float hash11(float n) { return fract(sin(n * 127.1) * 43758.5453); }
float jb_pt(vec2 p, vec2 c, float r) { return step(length(p - c), r); }
float boxAt(float k) { return step(0.45, hash11(k * 2.3 + 1.7)); }   // ~random blocks

// One pixel of the '?' glyph in block-local screen coords (y grows downward).
float qbit(vec2 g, float cx, float cy) {
    const float QC = 0.0052;
    return jb_box(g - vec2(cx * QC, cy * QC), vec2(QC * 0.5, QC * 0.5));
}
float qmark(vec2 g) {
    float m = 0.0;
    m = max(m, qbit(g, -1.0, -3.0)); m = max(m, qbit(g, 0.0, -3.0)); m = max(m, qbit(g, 1.0, -3.0));
    m = max(m, qbit(g, -2.0, -2.0)); m = max(m, qbit(g, 2.0, -2.0));
    m = max(m, qbit(g,  2.0, -1.0));
    m = max(m, qbit(g,  1.0,  0.0));
    m = max(m, qbit(g,  0.0,  1.0));
    m = max(m, qbit(g,  0.0,  3.0));
    return m;
}

void mainImage(out vec4 fragColor, in vec2 fragCoord) {
    vec2 uv = fragCoord / iResolution.xy;
    vec4 term = texture(iChannel0, uv);
    float aspect = iResolution.x / iResolution.y;
    vec2 p = vec2(uv.x * aspect, uv.y);

    float ground = 0.82;
    float bspace = 0.30;
    float v = 0.15;                         // world scroll speed

    vec3 acc = vec3(0.0);                   // scene, composited back-to-front

    // Clouds: white puffs drifting slowly across the sky.
    for (int i = 0; i < 3; i++) {
        float fi = float(i);
        float cx = fract(hash11(fi + 2.0) - SPOOKI_TIME * 0.008) * (aspect + 0.4) - 0.2;
        float cy = 0.15 + 0.10 * hash11(fi * 3.0);
        vec2 cc = p - vec2(cx, cy);
        float cl = jb_pt(cc, vec2(0.0, 0.0), 0.045);
        cl = max(cl, jb_pt(cc, vec2( 0.05, 0.012), 0.034));
        cl = max(cl, jb_pt(cc, vec2(-0.05, 0.012), 0.030));
        cl = max(cl, jb_pt(cc, vec2( 0.0,  0.02),  0.040));
        acc = mix(acc, vec3(0.95, 0.97, 1.0), cl);
    }

    // Green hills behind (clipped to the surface by the ground band below).
    for (int i = 0; i < 2; i++) {
        float fi = float(i);
        float hx = (0.22 + 0.42 * fi) * aspect;
        float R = 0.13 + 0.03 * fi;
        float hd = step(length((p - vec2(hx, ground)) / vec2(1.0, 0.85)), R)
                 * step(p.y, ground);
        acc = mix(acc, vec3(0.16, 0.52, 0.16), hd);
    }

    // Bushes on the ground (three-lobed).
    for (int i = 0; i < 3; i++) {
        float fi = float(i);
        float bxs = fract(hash11(fi * 5.0 + 1.0) - SPOOKI_TIME * 0.02) * (aspect + 0.4) - 0.2;
        vec2 bp = p - vec2(bxs, ground);
        float bush = jb_pt(bp, vec2(-0.03, 0.0), 0.026);
        bush = max(bush, jb_pt(bp, vec2(0.03, 0.0), 0.026));
        bush = max(bush, jb_pt(bp, vec2(0.0, -0.012), 0.032));
        bush *= step(p.y, ground + 0.004);
        acc = mix(acc, vec3(0.22, 0.62, 0.20), bush);
    }

    // Brick ground band along the bottom (running-bond mortar).
    float below = step(ground, p.y);
    float ry = (p.y - ground) / 0.028;
    float grow = floor(ry);
    float goff = mod(grow, 2.0) * 0.03;
    float gux = fract((p.x + goff) / 0.06);
    float gmort = step(0.90, gux) + step(fract(ry), 0.10);
    vec3 gcol = mix(vec3(0.82, 0.36, 0.16), vec3(0.45, 0.16, 0.06), clamp(gmort, 0.0, 1.0));
    acc = mix(acc, gcol, below);

    // Hero jump sync: he only hops when a block sits in his column.
    float xm = 0.35 * aspect;
    float worldXm = xm + SPOOKI_TIME * v;
    float k0 = floor(worldXm / bspace);
    float f0 = worldXm / bspace - k0;
    float present0 = boxAt(k0);
    float hop = present0 * 4.0 * f0 * (1.0 - f0) * 0.20;   // parabola, peak under the block
    float hitPulse = present0 * smoothstep(0.32, 0.5, f0) * smoothstep(0.68, 0.5, f0);

    // Blocks: random ? blocks and brick blocks; the hero's block bumps on a hit.
    float worldX = p.x + SPOOKI_TIME * v;
    float kb = floor(worldX / bspace);
    float present = boxAt(kb);
    float isBrick = step(0.5, hash11(kb * 1.7 + 9.3));
    float cx = (kb + 0.5) * bspace - SPOOKI_TIME * v;
    float bump = ((abs(kb - k0) < 0.5) ? 1.0 : 0.0) * hitPulse * 0.014;
    vec2 bc = p - vec2(cx, 0.5 - bump);
    float inblock = step(abs(bc.x), 0.035) * step(abs(bc.y), 0.035) * present;
    // Question block: orange with corner rivets and a dark '?'.
    vec3 qcol = vec3(0.93, 0.58, 0.11);
    float rivet = jb_pt(bc, vec2(-0.028, -0.028), 0.005) + jb_pt(bc, vec2(0.028, -0.028), 0.005)
                + jb_pt(bc, vec2(-0.028,  0.028), 0.005) + jb_pt(bc, vec2(0.028,  0.028), 0.005);
    qcol = mix(qcol, vec3(0.55, 0.30, 0.05), clamp(rivet, 0.0, 1.0));
    qcol = mix(qcol, vec3(0.30, 0.16, 0.03), qmark(bc));
    // Brick block: red with running-bond mortar.
    float bry = (bc.y + 0.035) / 0.0175;
    float brow = floor(bry);
    float broff = mod(brow, 2.0) * 0.0175;
    float bux = fract((bc.x + 0.035 + broff) / 0.035);
    float bmort = step(0.90, bux) + step(fract(bry), 0.12);
    vec3 brk = mix(vec3(0.80, 0.32, 0.14), vec3(0.40, 0.14, 0.05), clamp(bmort, 0.0, 1.0));
    vec3 blkcol = mix(qcol, brk, isBrick);
    acc = mix(acc, blkcol, inblock);

    // Goomba shuffling along the ground.
    float ggx = fract(0.6 - SPOOKI_TIME * 0.03) * (aspect + 0.3) - 0.15;
    vec2 gg = p - vec2(ggx, ground);
    float gdome = step(length((gg - vec2(0.0, -0.020)) / vec2(1.15, 1.0)), 0.024);
    float gface = step(length((gg - vec2(0.0, -0.008)) / vec2(1.0, 0.75)), 0.018);
    float gbody = max(gdome, gface) * step(gg.y, 0.0);
    acc = mix(acc, vec3(0.55, 0.34, 0.14), gbody);
    float gew = jb_pt(gg, vec2(-0.010, -0.016), 0.006) + jb_pt(gg, vec2(0.010, -0.016), 0.006);
    acc = mix(acc, vec3(0.98, 0.98, 0.98), gbody * clamp(gew, 0.0, 1.0));
    float gep = jb_pt(gg, vec2(-0.008, -0.016), 0.0026) + jb_pt(gg, vec2(0.008, -0.016), 0.0026);
    acc = mix(acc, vec3(0.05, 0.03, 0.02), gbody * clamp(gep, 0.0, 1.0));
    float gfeet = step(abs(abs(gg.x) - 0.012), 0.007) * step(abs(gg.y + 0.001), 0.004);
    acc = mix(acc, vec3(0.25, 0.12, 0.05), gfeet);

    // Hero (sprite unchanged): walks the ground, hops into a block above him.
    float walk = (1.0 - present0) * 0.004 * abs(sin(SPOOKI_TIME * 8.0));
    float footY = ground - hop - walk;
    vec2 q = vec2(p.x - xm, footY - p.y);
    vec3 mc; float ma;
    mario_draw(q, mc, ma);
    acc = mix(acc, mc, ma);

    float lum = dot(term.rgb, vec3(0.2126, 0.7152, 0.0722));
    float bgmask = 1.0 - smoothstep(0.05, 0.15, lum);
    vec3 res = term.rgb + acc * 0.6 * bgmask;
    fragColor = spooki_out(res, term);
}
