package places

import (
	"net/url"
	"regexp"
	"strings"

	"github.com/ikigenba/ikigenba/candidates/internal/sources"
)

var stateZIP = regexp.MustCompile(`^[A-Z]{2} [0-9]{5}(-[0-9]{4})?$`)

// Address splits a formatted US address, retaining unrecognised input as street.
func Address(formatted string) (street, city, state, zip string) {
	parts := []string{}
	for _, p := range strings.Split(formatted, ",") {
		if p = strings.TrimSpace(p); p != "" {
			parts = append(parts, p)
		}
	}
	if len(parts) > 0 && (parts[len(parts)-1] == "USA" || parts[len(parts)-1] == "United States") {
		parts = parts[:len(parts)-1]
	}
	if len(parts) < 2 || !stateZIP.MatchString(parts[len(parts)-1]) {
		return strings.TrimSpace(formatted), "", "", ""
	}
	last := parts[len(parts)-1]
	return strings.Join(parts[:len(parts)-2], ", "), parts[len(parts)-2], last[:2], last[3:8]
}

var sharedHosts = strings.Fields("facebook.com fb.com instagram.com linkedin.com twitter.com x.com youtube.com tiktok.com pinterest.com linktr.ee google.com goo.gl business.site wixsite.com weebly.com squarespace.com godaddysites.com wordpress.com blogspot.com square.site carrd.co yelp.com yellowpages.com bbb.org mapquest.com nextdoor.com angi.com homeadvisor.com houzz.com thumbtack.com manta.com chamberofcommerce.com etsy.com amazon.com ebay.com")

// DomainKey returns a website's non-shared, non-free host as a matching key.
func DomainKey(website string) string {
	u, err := url.Parse(website)
	if err != nil || u.Hostname() == "" {
		return ""
	}
	h := strings.TrimPrefix(strings.ToLower(u.Hostname()), "www.")
	for _, shared := range sharedHosts {
		if h == shared || strings.HasSuffix(h, "."+shared) {
			return ""
		}
	}
	return sources.DomainKey("@" + h)
}

var categoryTypes = map[string]string{}

func init() {
	groups := map[string]string{
		sources.CategoryConstruction:        "general_contractor electrician plumber roofing_contractor painter",
		sources.CategoryTrucking:            "moving_company courier_service storage truck_stop",
		sources.CategoryAgriculture:         "farm ranch",
		sources.CategoryManufacturing:       "manufacturer",
		sources.CategoryWholesale:           "wholesaler",
		sources.CategoryUtilities:           "electric_utility",
		sources.CategoryRetail:              "store hardware_store home_improvement_store furniture_store home_goods_store electronics_store clothing_store convenience_store department_store supermarket grocery_store liquor_store pet_store shoe_store sporting_goods_store jewelry_store book_store bicycle_store auto_parts_store cell_phone_store discount_store gift_shop shopping_mall warehouse_store florist car_dealer gas_station pharmacy drugstore",
		sources.CategoryOtherTransportation: "airport bus_station train_station transit_station taxi_stand car_rental",
		sources.CategoryInformation:         "telecommunications_service_provider",
		sources.CategoryFinance:             "bank atm insurance_agency",
		sources.CategoryRealEstate:          "real_estate_agency",
		sources.CategoryProfessional:        "accounting lawyer consultant veterinary_care",
		sources.CategoryAdministrative:      "travel_agency tour_agency",
		sources.CategoryEducation:           "school primary_school secondary_school university preschool",
		sources.CategoryHealthCare:          "hospital doctor dentist physiotherapist",
		sources.CategoryArts:                "gym museum art_gallery golf_course bowling_alley amusement_park",
		sources.CategoryFood:                "restaurant cafe bar bakery meal_takeaway meal_delivery fast_food_restaurant lodging hotel motel campground rv_park",
		sources.CategoryRepair:              "car_repair car_wash hair_care hair_salon beauty_salon barber_shop laundry locksmith funeral_home nail_salon",
		sources.CategoryReligious:           "church place_of_worship mosque synagogue hindu_temple",
		sources.CategoryGovernment:          "city_hall courthouse police fire_station post_office local_government_office embassy",
	}
	for category, types := range groups {
		for _, t := range strings.Fields(types) {
			categoryTypes[t] = category
		}
	}
}

// Category returns the weak category hint for an exact primary type word.
func Category(primaryType string) string { return categoryTypes[primaryType] }

// Reading interprets a place without giving it government or registry evidence.
func Reading(p Place) sources.Reading {
	r := sources.Reading{Name: p.Name, Names: []string{}, NameKey: sources.NameKey(p.Name), FleetVerdict: sources.FleetUnknown, People: []sources.Person{}, Cargo: []string{}, Evidence: []sources.Evidence{}, DomainKey: DomainKey(p.Website), Category: Category(p.PrimaryType)}
	if p.Name != "" {
		r.Names = append(r.Names, p.Name)
	}
	r.NameKeys = sources.NameKeys(r.Names)
	r.Street, r.City, r.State, r.Zip = Address(p.Address)
	if key := sources.PhoneKey(p.Phone); len(key) == 10 {
		r.Phone, r.PhoneKey = key, key
	}
	return r
}
