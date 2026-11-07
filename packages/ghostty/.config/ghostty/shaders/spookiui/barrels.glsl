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

// SpookiUI treat: Barrels.
// Barrels tumble down slanted girders — a wink at the 1981 platform arcade
// classic. Original SpookiUI shader: plain shapes, no game artwork. Drawn over
// dark background pixels only.

float h(float n) { return fract(sin(n) * 43758.5453); }

void mainImage(out vec4 fragColor, in vec2 fragCoord) {
    vec2 uv = fragCoord / iResolution.xy;
    vec4 term = texture(iChannel0, uv);
    float aspect = iResolution.x / iResolution.y;
    vec2 p = vec2(uv.x * aspect, uv.y);

    vec3 acc = vec3(0.0);
    const int LEVELS = 4;
    float slope = 0.06;

    // Girders: tilted bands, alternating slope direction on each level.
    for (int i = 0; i < LEVELS; i++) {
        float fi = float(i);
        float dir = (mod(fi, 2.0) < 1.0) ? 1.0 : -1.0;
        float gy = 0.12 + 0.24 * fi + slope * dir * (p.x / aspect);
        acc += vec3(0.9, 0.3, 0.25)
             * (1.0 - smoothstep(0.006, 0.014, abs(p.y - gy))) * 0.5;
    }

    // Barrels: roll along a level, then drop to the next, staggered in time.
    const int BARRELS = 5;
    for (int b = 0; b < BARRELS; b++) {
        float fb = float(b);
        float lvlF = fract(SPOOKI_TIME * 0.12 + h(fb * 3.0)) * float(LEVELS - 1);
        float lvl = floor(lvlF);
        float frac = fract(lvlF);
        float dir = (mod(lvl, 2.0) < 1.0) ? 1.0 : -1.0;
        float x = ((dir > 0.0) ? frac : 1.0 - frac) * aspect;
        // Sit on TOP of the girder. Screen y grows downward, so "above the
        // slope" is a smaller y — subtract the offset.
        float y = 0.12 + 0.24 * lvl + slope * dir * (x / aspect) - 0.03;
        vec2 bp = p - vec2(x, y);
        float barrel = 1.0 - smoothstep(0.022, 0.028, length(bp));
        float roll = 0.5 + 0.5 * sin(atan(bp.y, bp.x) * 3.0 + SPOOKI_TIME * 8.0 * dir);
        acc += mix(vec3(0.8, 0.55, 0.2), vec3(0.5, 0.3, 0.1), roll) * barrel;
    }

    float lum = dot(term.rgb, vec3(0.2126, 0.7152, 0.0722));
    float bgmask = 1.0 - smoothstep(0.05, 0.15, lum);
    vec3 res = term.rgb + acc * 0.6 * bgmask;
    fragColor = spooki_out(res, term);
}
