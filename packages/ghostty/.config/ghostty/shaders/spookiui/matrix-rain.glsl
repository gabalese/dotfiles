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

// SpookiUI treat: Matrix Rain.
// Falling green glyph columns in the spirit of `cmatrix`. Drawn only over dark
// background pixels so your text stays readable. Original SpookiUI shader.

const float COLUMNS = 46.0;

float rand(vec2 p) {
    p = fract(p * vec2(123.34, 456.21));
    p += dot(p, p + 45.32);
    return fract(p.x * p.y);
}

void mainImage(out vec4 fragColor, in vec2 fragCoord) {
    vec2 uv = fragCoord / iResolution.xy;
    vec4 term = texture(iChannel0, uv);

    float cw = iResolution.x / COLUMNS;          // column width (px)
    float col = floor(fragCoord.x / cw);
    float chH = cw * 1.5;                         // glyph cell height (px)
    float rows = iResolution.y / chH;

    // Each column's stream head sweeps top->bottom at its own speed/phase.
    float speed = mix(0.12, 0.45, rand(vec2(col, 2.0)));
    float seed = rand(vec2(col, 9.0));
    float phase = fract(SPOOKI_TIME * speed + seed);
    float head = 1.0 - phase;                     // uv.y of the head
    float trail = mix(0.25, 0.60, rand(vec2(col, 5.0)));
    float d = head - uv.y;                         // >0 for the trailing tail
    float body = (d >= 0.0 && d <= trail) ? (1.0 - d / trail) : 0.0;
    body = pow(body, 1.4);

    // Per-cell glyph flicker + a sub-cell mask to read as characters.
    float cellRow = floor(uv.y * rows);
    float g = rand(vec2(col, cellRow) + floor(SPOOKI_TIME * mix(6.0, 14.0, seed)));
    float glyph = step(0.35, g);
    vec2 f = fract(vec2(fragCoord.x / cw, uv.y * rows));
    float mask = step(0.12, f.x) * step(f.x, 0.88)
               * step(0.10, f.y) * step(f.y, 0.90);
    float lit = body * glyph * mask;

    // The leading glyph glows near-white; the tail is green.
    float headGlow = smoothstep(0.05, 0.0, abs(d)) * glyph * mask;
    vec3 green = vec3(0.15, 1.0, 0.30);
    vec3 rain = green * lit * 0.42 + vec3(0.80, 1.0, 0.85) * headGlow * 0.45;

    float lum = dot(term.rgb, vec3(0.2126, 0.7152, 0.0722));
    float bgmask = 1.0 - smoothstep(0.05, 0.14, lum);

    vec3 res = term.rgb + rain * bgmask;
    fragColor = spooki_out(res, term);
}
