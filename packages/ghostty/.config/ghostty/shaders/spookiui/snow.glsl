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

// SpookiUI treat: Snow.
// Soft snowflakes drift down and sway across a few depth layers. Original
// SpookiUI shader; drawn over dark background pixels only.

float sn(vec2 p) {
    p = fract(p * vec2(127.1, 311.7));
    p += dot(p, p + 45.32);
    return fract(p.x * p.y);
}

float snowLayer(vec2 uv, float aspect, float t, float scale, float speed, float sz) {
    vec2 g = vec2(uv.x * aspect, uv.y) * scale;
    g.y -= t * speed;                       // fall downward (uv.y grows down)
    g.x += 0.4 * sin(t * 0.5 + g.y * 0.6);  // gentle sway
    vec2 cell = floor(g);
    vec2 f = fract(g) - 0.5;
    float r = sn(cell);
    vec2 off = 0.3 * vec2(sin(r * 30.0), cos(r * 21.0));
    float d = length(f - off);
    return smoothstep(sz, 0.0, d) * step(0.45, r);   // ~55% of cells carry a flake
}

void mainImage(out vec4 fragColor, in vec2 fragCoord) {
    vec2 uv = fragCoord / iResolution.xy;
    vec4 term = texture(iChannel0, uv);
    float aspect = iResolution.x / iResolution.y;

    float s = snowLayer(uv, aspect, SPOOKI_TIME, 9.0,  0.35, 0.13) * 0.7
            + snowLayer(uv, aspect, SPOOKI_TIME, 15.0, 0.6,  0.10) * 0.85
            + snowLayer(uv, aspect, SPOOKI_TIME, 23.0, 0.9,  0.08);
    vec3 snow = vec3(0.95, 0.97, 1.0) * clamp(s, 0.0, 1.0);

    float lum = dot(term.rgb, vec3(0.2126, 0.7152, 0.0722));
    float bgmask = 1.0 - smoothstep(0.05, 0.14, lum);

    vec3 res = term.rgb + snow * 0.6 * bgmask;
    fragColor = spooki_out(res, term);
}
