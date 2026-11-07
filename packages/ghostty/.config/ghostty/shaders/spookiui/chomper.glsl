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

// SpookiUI treat: Chomper.
// A yellow chomping wedge munches a row of pellets while a ghost gives chase — a
// fond wink at the 1980 maze arcade classic. Original SpookiUI shader: plain
// shapes, no game artwork. Drawn over dark background pixels only.

void mainImage(out vec4 fragColor, in vec2 fragCoord) {
    vec2 uv = fragCoord / iResolution.xy;
    vec4 term = texture(iChannel0, uv);
    float aspect = iResolution.x / iResolution.y;
    vec2 p = vec2(uv.x * aspect, uv.y);

    float row = 0.5;
    float r = 0.05;
    float loop = aspect + 0.4;
    float px = mod(SPOOKI_TIME * 0.22, loop) - 0.2;          // chomper enters from left
    vec3 acc = vec3(0.0);

    // Pellets: evenly spaced dots, eaten once the chomper has passed them.
    float spacing = 0.14;
    float gx = floor(p.x / spacing) * spacing + spacing * 0.5;
    if (gx > px + r) {
        acc += vec3(1.0, 0.85, 0.55)
             * (1.0 - smoothstep(0.010, 0.016, length(p - vec2(gx, row))));
    }

    // Chomper: a disc with an opening/closing mouth wedge facing right.
    vec2 rel = p - vec2(px, row);
    float dc = length(rel);
    float mouth = 0.62 * (0.5 + 0.5 * sin(SPOOKI_TIME * 10.0));   // half-angle of the gap
    float body = (dc < r && abs(atan(rel.y, rel.x)) > mouth) ? 1.0 : 0.0;
    body *= smoothstep(r, r - 0.006, dc);
    acc += vec3(1.0, 0.92, 0.15) * body;

    // Ghost chasing behind — the same sprite as the Ghosts treat (rounded
    // dome, wavy skirt, two eye holes), bobbing as it trails the chomper.
    vec2 gr = p - vec2(px - 0.16, row + 0.012 * sin(SPOOKI_TIME * 6.0));
    float ghost = ghost_body(gr, r, SPOOKI_TIME * 3.0);
    acc += vec3(0.72, 0.80, 1.0) * ghost;

    float lum = dot(term.rgb, vec3(0.2126, 0.7152, 0.0722));
    float bgmask = 1.0 - smoothstep(0.05, 0.15, lum);
    vec3 res = term.rgb + acc * 0.7 * bgmask;
    fragColor = spooki_out(res, term);
}
