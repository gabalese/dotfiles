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

// Shared SpookiUI ghost sprite — the SAME look used by the Ghosts treat and by
// the Chomper treat's chaser: a rounded dome with straight sides, a wavy skirt
// hem, and two eye holes. `p` is ghost-local and aspect-corrected (p.y < 0 is
// up); `r` is the radius; `wob` drives the skirt's waver. Returns coverage 0..1.
float ghost_body(vec2 p, float r, float wob) {
    float dome = (p.y < 0.0) ? length(p) : abs(p.x);            // rounded top, straight sides
    float hem = 1.05 * r + 0.10 * r * sin(p.x / r * 8.0 + wob); // wavy skirt
    float shape = step(dome, r) * step(p.y, hem);
    float eyes = smoothstep(0.28 * r, 0.14 * r, length(p - vec2(-0.34 * r, -0.18 * r)))
               + smoothstep(0.28 * r, 0.14 * r, length(p - vec2( 0.34 * r, -0.18 * r)));
    return clamp(shape - eyes, 0.0, 1.0);
}

// SpookiUI treat: Ghosts.
// Friendly cartoon ghosts drift across the screen, bobbing and fading in/out at
// the edges. Original SpookiUI shader; drawn over dark background pixels only.

float gh(float n) { return fract(sin(n * 12.9898) * 43758.5453123); }

void mainImage(out vec4 fragColor, in vec2 fragCoord) {
    vec2 uv = fragCoord / iResolution.xy;
    vec4 term = texture(iChannel0, uv);
    float aspect = iResolution.x / iResolution.y;

    vec3 acc = vec3(0.0);
    const int N = 4;
    for (int i = 0; i < N; i++) {
        float fi = float(i);
        float speed = 0.03 + 0.05 * gh(fi + 1.3);
        float x = fract(gh(fi * 2.1) + SPOOKI_TIME * speed);
        float y = 0.2 + 0.6 * gh(fi * 3.7) + 0.04 * sin(SPOOKI_TIME * (0.9 + 0.3 * fi) + fi);
        float r = 0.06 + 0.025 * gh(fi * 5.5);

        // ghost-local, aspect-corrected. uv.y grows downward, so p.y < 0 is up.
        vec2 p = vec2((uv.x - x) * aspect, uv.y - y);
        float body = ghost_body(p, r, SPOOKI_TIME * 3.0);
        float fade = smoothstep(0.0, 0.08, x) * smoothstep(1.0, 0.9, x);  // hide the wrap
        acc += vec3(0.72, 0.80, 1.0) * body * fade;
    }

    float lum = dot(term.rgb, vec3(0.2126, 0.7152, 0.0722));
    float bgmask = 1.0 - smoothstep(0.05, 0.14, lum);

    vec3 res = term.rgb + acc * 0.5 * bgmask;
    fragColor = spooki_out(res, term);
}
